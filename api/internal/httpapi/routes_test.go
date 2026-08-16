package httpapi

import (
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sdeitte/survivor-league-api/internal/auth"
)

// middlewareName returns a stable identifier for a middleware func — e.g.
// "github.com/.../httpapi.(*API).RequireLeagueMember-fm" for
// a.RequireLeagueMember. Every bound-method value of a given (type,
// method) pair shares this name regardless of receiver, which is what
// lets this test check "does this route's chain include
// RequireCommissioner" via a plain substring match rather than any
// pointer/receiver-identity trick.
func middlewareName(mw func(http.Handler) http.Handler) string {
	return runtime.FuncForPC(reflect.ValueOf(mw).Pointer()).Name()
}

// buildTestRouter builds a real router via the same NewRouter used by
// cmd/server, with nil DB-backed dependencies. That's safe here because
// this test only walks the *static* route/middleware table (chi.Walk below
// never dispatches a request through a handler) — it's a structural
// regression guard, not an integration test.
func buildTestRouter(t *testing.T) chi.Routes {
	t.Helper()
	router := NewRouter(Deps{
		JWT:               auth.NewJWTIssuer("test-secret"),
		AppEnv:            "development",
		CORSAllowedOrigin: "http://localhost:5173",
	})
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("NewRouter did not return a chi.Routes-compatible handler")
	}
	return routes
}

type routeEntry struct {
	method      string
	route       string
	middlewares []string
}

func walkRoutes(t *testing.T, routes chi.Routes) []routeEntry {
	t.Helper()
	var entries []routeEntry
	err := chi.Walk(routes, func(method, route string, handler http.Handler, mws ...func(http.Handler) http.Handler) error {
		names := make([]string, 0, len(mws))
		for _, mw := range mws {
			names = append(names, middlewareName(mw))
		}
		entries = append(entries, routeEntry{method: method, route: route, middlewares: names})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return entries
}

func hasMiddleware(names []string, suffix string) bool {
	for _, n := range names {
		if strings.Contains(n, suffix) {
			return true
		}
	}
	return false
}

// TestRouteTable_LeagueRoutesRequireAtLeastMembership is the Phase 2
// extension of the plan's "Auth & RBAC" regression-guard pattern (Phase 1
// built requireAuth/requireSiteAdmin but had no league routes yet to test
// against). It asserts every /leagues/{id}/... route carries at least
// RequireLeagueMember — RequireCommissioner also satisfies this since it
// wraps RequireLeagueMember internally — a direct guard against the old
// app's exact failure mode of unguarded per-league/admin endpoints.
//
// Note: chi.Walk normalizes the bare "/leagues/{id}" route (registered as
// pattern "/" inside r.Route("/leagues/{id}", ...)) to "/leagues/{id}/"
// with a trailing slash — confirmed empirically, not just inferred from
// chi's source. The HasPrefix check below is deliberately insensitive to
// that.
func TestRouteTable_LeagueRoutesRequireAtLeastMembership(t *testing.T) {
	entries := walkRoutes(t, buildTestRouter(t))

	checked := 0
	for _, e := range entries {
		if !strings.HasPrefix(e.route, "/leagues/{id}") {
			continue
		}
		checked++
		if !hasMiddleware(e.middlewares, "RequireLeagueMember") && !hasMiddleware(e.middlewares, "RequireCommissioner") {
			t.Errorf("%s %s does not carry RequireLeagueMember or RequireCommissioner; middlewares=%v", e.method, e.route, e.middlewares)
		}
	}
	if checked == 0 {
		t.Fatal("no /leagues/{id}/... routes found in the route table — has the route table changed shape?")
	}
}

// TestRouteTable_CommissionerOnlyRoutesRequireCommissioner asserts the
// commissioner-scoped subset of league routes specifically requires
// RequireCommissioner, not merely RequireLeagueMember.
func TestRouteTable_CommissionerOnlyRoutesRequireCommissioner(t *testing.T) {
	entries := walkRoutes(t, buildTestRouter(t))

	want := map[string]bool{
		"PATCH /leagues/{id}/":                        false,
		"DELETE /leagues/{id}/members/{membershipId}": false,
		"GET /leagues/{id}/invite":                    false,
		"POST /leagues/{id}/invite/regenerate":        false,
	}

	for _, e := range entries {
		key := e.method + " " + e.route
		if _, tracked := want[key]; !tracked {
			continue
		}
		want[key] = true
		if !hasMiddleware(e.middlewares, "RequireCommissioner") {
			t.Errorf("%s does not carry RequireCommissioner; middlewares=%v", key, e.middlewares)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("expected commissioner-only route %q not found in route table — has it moved or been renamed?", key)
		}
	}
}

// TestRouteTable_MemberOnlyRoutesDoNotOverRestrict is a sanity check the
// other direction: member-only routes (GET /leagues/{id}, GET
// /leagues/{id}/members) must carry RequireLeagueMember but must NOT be
// accidentally over-restricted to RequireCommissioner.
func TestRouteTable_MemberOnlyRoutesDoNotOverRestrict(t *testing.T) {
	entries := walkRoutes(t, buildTestRouter(t))

	want := map[string]bool{
		"GET /leagues/{id}/":        false,
		"GET /leagues/{id}/members": false,
	}

	for _, e := range entries {
		key := e.method + " " + e.route
		if _, tracked := want[key]; !tracked {
			continue
		}
		want[key] = true
		if !hasMiddleware(e.middlewares, "RequireLeagueMember") {
			t.Errorf("%s does not carry RequireLeagueMember; middlewares=%v", key, e.middlewares)
		}
		if hasMiddleware(e.middlewares, "RequireCommissioner") {
			t.Errorf("%s unexpectedly requires RequireCommissioner (should be member-only)", key)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("expected member-only route %q not found in route table", key)
		}
	}
}

// TestRouteTable_InvitesAndTopLevelLeagueRoutes double-checks the routes
// surrounding /leagues/{id}/...: GET /invites/{code} must stay public
// (no requireAuth) since unauthenticated users need to preview an invite
// before registering/logging in; POST /invites/{code}/join and POST/GET
// /leagues require auth but must NOT require league membership (there's
// no league membership to check yet — /leagues is where it gets created,
// /invites/{code}/join is where it's granted).
func TestRouteTable_InvitesAndTopLevelLeagueRoutes(t *testing.T) {
	entries := walkRoutes(t, buildTestRouter(t))

	checkedPublic := false
	checkedAuthOnly := 0
	for _, e := range entries {
		key := e.method + " " + e.route
		switch key {
		case "GET /invites/{code}":
			checkedPublic = true
			if hasMiddleware(e.middlewares, "RequireAuth") {
				t.Errorf("%s should be public, but requires auth; middlewares=%v", key, e.middlewares)
			}
			if hasMiddleware(e.middlewares, "RequireLeagueMember") {
				t.Errorf("%s should be public, but requires league membership; middlewares=%v", key, e.middlewares)
			}
		case "POST /invites/{code}/join", "POST /leagues", "GET /leagues":
			checkedAuthOnly++
			if !hasMiddleware(e.middlewares, "RequireAuth") {
				t.Errorf("%s should require auth; middlewares=%v", key, e.middlewares)
			}
			if hasMiddleware(e.middlewares, "RequireLeagueMember") {
				t.Errorf("%s should not require league membership; middlewares=%v", key, e.middlewares)
			}
		}
	}
	if !checkedPublic {
		t.Error("GET /invites/{code} not found in route table")
	}
	if checkedAuthOnly != 3 {
		t.Errorf("expected to check 3 auth-only routes (POST /invites/{code}/join, POST /leagues, GET /leagues), checked %d", checkedAuthOnly)
	}
}
