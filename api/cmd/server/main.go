// Command server starts the Survivor League HTTP API.
//
// Phase 1: auth (register/login/refresh/logout), GET/PATCH /me, and the
// requireAuth/requireSiteAdmin middleware, alongside the /health check.
// Route wiring lives in internal/httpapi; this file just reads
// configuration from the environment and assembles dependencies.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/httpapi"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := getenv("PORT", "8080")
	appEnv := getenv("APP_ENV", "development")
	corsAllowedOrigin := getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173")
	adminEmail := os.Getenv("ADMIN_EMAIL")

	// Both are required from Phase 1 onward: every route except /health
	// needs a database, and no token can be issued/verified without a
	// signing secret. Fail fast rather than booting into a half-broken state.
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL must be set (see .env.example / docker-compose.yml for local dev)")
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET must be set (see .env.example for a dev-only default)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		log.Fatalf("failed to configure database pool: %v", err)
	}
	defer pool.Close()

	jwtIssuer := auth.NewJWTIssuer(jwtSecret)
	queries := gen.New(pool)
	authService := auth.NewService(queries, jwtIssuer, adminEmail)

	router := httpapi.NewRouter(httpapi.Deps{
		Pool:              pool,
		AuthService:       authService,
		JWT:               jwtIssuer,
		AppEnv:            appEnv,
		CORSAllowedOrigin: corsAllowedOrigin,
	})

	addr := ":" + port
	log.Printf("survivor-league-api listening on %s (env=%s, cors_origin=%s)", addr, appEnv, corsAllowedOrigin)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
