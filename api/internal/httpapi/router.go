package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/admin"
	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/notify"
	"github.com/sdeitte/survivor-league-api/internal/picks"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

// Deps are the dependencies NewRouter needs to wire up routes/middleware.
type Deps struct {
	Pool              *pgxpool.Pool
	AuthService       *auth.Service
	LeaguesService    *leagues.Service
	ScheduleService   *schedule.Service
	AdminService      *admin.Service
	PicksService      *picks.Service
	NotifyService     *notify.Service
	JWT               *auth.JWTIssuer
	AppEnv            string // "development" | "production" — gates cookie Secure flag
	CORSAllowedOrigin string
}

// API holds the dependencies shared by the httpapi handlers/middleware.
type API struct {
	pool            *pgxpool.Pool
	authService     *auth.Service
	leaguesService  *leagues.Service
	scheduleService *schedule.Service
	adminService    *admin.Service
	picksService    *picks.Service
	notifyService   *notify.Service
	jwt             *auth.JWTIssuer
	appEnv          string
}

// NewRouter builds the full chi router: middleware stack, CORS, and all
// routes for this phase (health, auth, me, leagues/invites/conferences,
// schedule reads, admin schedule sync, picks, leaderboard, buy-back,
// device tokens/notification preferences).
func NewRouter(d Deps) http.Handler {
	a := &API{
		pool:            d.Pool,
		authService:     d.AuthService,
		leaguesService:  d.LeaguesService,
		scheduleService: d.ScheduleService,
		adminService:    d.AdminService,
		picksService:    d.PicksService,
		notifyService:   d.NotifyService,
		jwt:             d.JWT,
		appEnv:          d.AppEnv,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Timeout(30 * time.Second))

	// Tightened from Phase 0's permissive `*` origin: cookies
	// (credentials: 'include', needed for the refresh_token cookie to
	// round-trip) don't work with a wildcard origin per browser spec, so
	// this is now a single explicit origin sourced from CORS_ALLOWED_ORIGIN.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{d.CORSAllowedOrigin},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", a.handleHealth)

	r.Route("/auth", func(r chi.Router) {
		r.Post("/register", a.handleRegister)
		r.Post("/login", a.handleLogin)
		r.Post("/refresh", a.handleRefresh)
		r.Post("/logout", a.handleLogout)
	})

	r.With(a.RequireAuth).Get("/me", a.handleGetMe)
	r.With(a.RequireAuth).Patch("/me", a.handleUpdateMe)
	r.With(a.RequireAuth).Post("/me/device-tokens", a.handleRegisterDeviceToken)
	r.With(a.RequireAuth).Delete("/me/device-tokens", a.handleDeleteDeviceToken)
	r.With(a.RequireAuth).Get("/me/notification-preferences", a.handleGetNotificationPreferences)
	r.With(a.RequireAuth).Put("/me/notification-preferences", a.handleUpdateNotificationPreferences)

	r.Get("/conferences", a.handleListConferences)

	r.With(a.RequireAuth).Post("/leagues", a.handleCreateLeague)
	r.With(a.RequireAuth).Get("/leagues", a.handleListLeagues)

	r.Route("/leagues/{id}", func(r chi.Router) {
		r.With(a.RequireLeagueMember).Get("/", a.handleGetLeague)
		r.With(a.RequireCommissioner).Patch("/", a.handleUpdateLeague)
		r.With(a.RequireLeagueMember).Get("/members", a.handleListMembers)
		r.With(a.RequireLeagueMember).Get("/leaderboard", a.handleGetLeaderboard)
		r.With(a.RequireCommissioner).Delete("/members/{membershipId}", a.handleRemoveMember)
		r.With(a.RequireCommissioner).Post("/members/{membershipId}/buyback", a.handleBuyBackMember)
		r.With(a.RequireCommissioner).Get("/invite", a.handleGetInviteCode)
		r.With(a.RequireCommissioner).Post("/invite/regenerate", a.handleRegenerateInviteCode)

		r.Route("/weeks/{weekId}", func(r chi.Router) {
			r.With(a.RequireLeagueMember).Get("/picks/me", a.handleGetMyPick)
			r.With(a.RequireLeagueMember).Put("/picks/me", a.handleUpsertMyPick)
			r.With(a.RequireLeagueMember).Get("/available-teams", a.handleListAvailableTeams)
			r.With(a.RequireLeagueMember).Get("/picks", a.handleListWeekPicks)
		})
	})

	r.Get("/invites/{code}", a.handlePreviewInvite)
	r.With(a.RequireAuth).Post("/invites/{code}/join", a.handleJoinByCode)

	r.With(a.RequireAuth).Get("/weeks", a.handleListWeeks)
	r.With(a.RequireAuth).Get("/weeks/{id}/games", a.handleListWeekGames)
	r.With(a.RequireAuth).Get("/games/{id}", a.handleGetGame)
	r.With(a.RequireAuth).Get("/teams", a.handleListTeams)

	r.Route("/admin", func(r chi.Router) {
		r.With(a.RequireSiteAdmin).Post("/sync/schedule", a.handleTriggerScheduleSync)
		r.With(a.RequireSiteAdmin).Get("/sync/runs", a.handleListSyncRuns)
		r.With(a.RequireSiteAdmin).Get("/leagues", a.handleListLeaguesAdmin)
		r.With(a.RequireSiteAdmin).Get("/users", a.handleListUsersAdmin)
		r.With(a.RequireSiteAdmin).Post("/users/{id}/disable", a.handleDisableUser)
		r.With(a.RequireSiteAdmin).Post("/users/{id}/enable", a.handleEnableUser)
		r.With(a.RequireSiteAdmin).Post("/games/{id}/resync", a.handleResyncGame)
		r.With(a.RequireSiteAdmin).Get("/audit-log", a.handleListAuditLog)
	})

	return r
}
