// Command server starts the Survivor League HTTP API.
//
// Phase 1 added auth (register/login/refresh/logout), GET/PATCH /me, and
// the requireAuth/requireSiteAdmin middleware. Phase 2 added league CRUD,
// membership, and the invite-code join flow, plus the
// requireLeagueMember/requireCommissioner middleware. Phase 3 adds CFBD
// schedule ingestion (internal/schedule), the site-admin sync-trigger
// endpoints (internal/admin, the first real use of requireSiteAdmin), and a
// daily cron job that keeps the schedule in sync automatically — additive
// to the manual admin endpoint, not a replacement. Phase 5 adds the
// grading/elimination pipeline (internal/grading) and the adaptive live
// poll loop (internal/livepoll) that drives it. Phase 6 adds the
// commissioner buy-back endpoint. Phase 7 adds notifications
// (internal/notify): device tokens/preferences, the notification_outbox
// dispatcher ticker, and the hourly pick_reminder cron job, wired
// alongside the existing cron/poll-loop pattern. Phase 8 completes
// site-admin (internal/admin): cross-league leagues/users listing, user
// disable/enable, single-game CFBD resync (reusing internal/grading's
// GradeGame/TryFinalizeLeagueWeek — the reason adminService is now
// constructed after gradingService below), and the audit log viewer.
// Route wiring lives in internal/httpapi; this file just reads
// configuration from the environment, assembles dependencies, and owns
// process lifecycle (HTTP server + cron scheduler + live poll loop +
// notification dispatcher, all stopped cleanly on shutdown).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/sdeitte/survivor-league-api/internal/admin"
	"github.com/sdeitte/survivor-league-api/internal/auth"
	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/grading"
	"github.com/sdeitte/survivor-league-api/internal/httpapi"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
	"github.com/sdeitte/survivor-league-api/internal/livepoll"
	"github.com/sdeitte/survivor-league-api/internal/notify"
	"github.com/sdeitte/survivor-league-api/internal/picks"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// currentSeasonYear picks the season_year the daily cron sync should run
// against. College football seasons run August-January under a single
// season_year equal to the calendar year they start in, so during the
// Jan-Jun off-season "the current season" means the one that most recently
// finished/is about to start (last calendar year), not the current
// mid-season calendar year. July is treated as the start of the runway into
// a new season (fall camp announcements, early schedule data) rather than
// waiting for August.
func currentSeasonYear(now time.Time) int {
	if now.Month() >= time.July {
		return now.Year()
	}
	return now.Year() - 1
}

func main() {
	port := getenv("PORT", "8080")
	appEnv := getenv("APP_ENV", "development")
	corsAllowedOrigin := getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173")
	adminEmail := os.Getenv("ADMIN_EMAIL")

	// CFBD_BASE_URL is configurable specifically so it can be pointed at a
	// mock HTTP server for local/E2E testing — there is no live CFBD API
	// key in this environment yet (see internal/schedule/cfbd_client.go).
	// Defaults to the real collegefootballdata.com host.
	cfbdBaseURL := getenv("CFBD_BASE_URL", schedule.DefaultCFBDBaseURL)
	cfbdAPIKey := os.Getenv("CFBD_API_KEY")
	if cfbdAPIKey == "" {
		log.Print("warning: CFBD_API_KEY is not set — schedule sync calls to the real CFBD API will fail with 401 until a key is configured")
	}

	// EXPO_PUSH_BASE_URL/RESEND_BASE_URL are configurable for the same
	// reason CFBD_BASE_URL is above — pointed at a mock HTTP server for
	// local/E2E testing. RESEND_API_KEY has no real value in this
	// environment yet, same "flagged but unavailable" treatment as
	// CFBD_API_KEY: sends will fail (logged, retried up to the attempt
	// cap, then marked permanently failed) until a real key is
	// configured. EXPO_ACCESS_TOKEN is Expo's optional "enhanced
	// security" feature — push delivery itself needs no token, so an
	// empty value is a normal, fully-functional configuration, not a
	// degraded one like the CFBD/Resend keys.
	expoPushBaseURL := getenv("EXPO_PUSH_BASE_URL", notify.DefaultExpoPushURL)
	expoAccessToken := os.Getenv("EXPO_ACCESS_TOKEN")
	resendBaseURL := getenv("RESEND_BASE_URL", notify.DefaultResendURL)
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	resendFromEmail := getenv("RESEND_FROM_EMAIL", "Survivor League <notifications@survivor-league.example>")
	if resendAPIKey == "" {
		log.Print("warning: RESEND_API_KEY is not set — email notifications will fail until a key is configured")
	}

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

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelSetup()

	pool, err := db.NewPool(setupCtx, databaseURL)
	if err != nil {
		log.Fatalf("failed to configure database pool: %v", err)
	}
	defer pool.Close()

	jwtIssuer := auth.NewJWTIssuer(jwtSecret)
	queries := gen.New(pool)
	authService := auth.NewService(queries, jwtIssuer, adminEmail)
	cfbdClient := schedule.NewCFBDClient(http.DefaultClient, cfbdBaseURL, cfbdAPIKey)
	scheduleService := schedule.NewService(queries, cfbdClient)
	picksService := picks.NewService(queries, pool)

	// --- Notifications (Phase 7) ---
	//
	// Constructed before leaguesService/gradingService since both take a
	// notify.Service as their Notifier (see internal/leagues.WithNotifier,
	// internal/grading.WithNotifier) — the trigger sites for eliminated/
	// survived/mass_wipeout/buyback notifications, called directly from
	// the real grading/buy-back pipelines, not a parallel/duplicate check.
	pushSender := notify.NewExpoPushSender(http.DefaultClient, expoPushBaseURL, expoAccessToken)
	emailSender := notify.NewResendEmailSender(http.DefaultClient, resendBaseURL, resendAPIKey, resendFromEmail)
	notifyService := notify.NewService(queries, pool, pushSender, emailSender)

	leaguesService := leagues.NewService(queries, pool, leagues.WithNotifier(notifyService))
	gradingService := grading.NewService(queries, pool, grading.WithNotifier(notifyService))

	// adminService is constructed after gradingService (Phase 8's single-
	// game resync path — POST /admin/games/:id/resync — reuses
	// grading.Service.GradeGame/TryFinalizeLeagueWeek directly, the same
	// functions internal/livepoll's poll loop calls).
	adminService := admin.NewService(queries, scheduleService, gradingService)

	router := httpapi.NewRouter(httpapi.Deps{
		Pool:              pool,
		AuthService:       authService,
		LeaguesService:    leaguesService,
		ScheduleService:   scheduleService,
		AdminService:      adminService,
		PicksService:      picksService,
		NotifyService:     notifyService,
		JWT:               jwtIssuer,
		AppEnv:            appEnv,
		CORSAllowedOrigin: corsAllowedOrigin,
	})

	// --- Daily schedule-sync cron (additive to the manual admin endpoint) ---
	//
	// Runs at 6:00 AM America/New_York — off-peak, well after any
	// late-night games have finished and their final scores/statuses have
	// settled, and before most users start checking picks for the day.
	// Falls back to UTC if the timezone database isn't available in the
	// runtime environment (rare, but cleaner than crashing the whole
	// server startup over the scheduler's timezone).
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Printf("warning: could not load America/New_York timezone (%v) — cron will run on UTC instead", err)
		loc = time.UTC
	}
	cronScheduler := cron.New(cron.WithLocation(loc))
	_, err = cronScheduler.AddFunc("0 6 * * *", func() {
		year := currentSeasonYear(time.Now())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		// triggeredBy is intentionally the zero value (invalid UUID): the
		// cron job has no acting user, so TriggerScheduleSync skips writing
		// an audit_log row for it (audit_log.actor_user_id would have
		// nothing meaningful to point at) while still recording the
		// sync_runs row with trigger="cron".
		if _, err := adminService.TriggerScheduleSync(ctx, pgtype.UUID{}, admin.TriggerCron, year); err != nil {
			log.Printf("cron schedule sync (season_year=%d) failed: %v", year, err)
		} else {
			log.Printf("cron schedule sync (season_year=%d) completed", year)
		}
	})
	if err != nil {
		log.Fatalf("failed to schedule cron job: %v", err)
	}

	// --- Hourly pick_reminder scan (Phase 7) ---
	//
	// Added to the same cron scheduler as the daily schedule sync above,
	// per the plan's "Hourly reminder scan for members without a pick
	// inside ~24h/~3h of their nearest lock". See
	// internal/notify.Service.ScanPickReminders for the full design —
	// dedupe keys (not this cron's own state) are what make firing every
	// hour safe.
	_, err = cronScheduler.AddFunc("0 * * * *", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := notifyService.ScanPickReminders(ctx); err != nil {
			log.Printf("cron pick_reminder scan failed: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("failed to schedule pick_reminder cron job: %v", err)
	}
	cronScheduler.Start()

	// --- Live poll loop (Phase 5) ---
	//
	// A single background ticker, separate from the daily cron above: it
	// checks every ~90s whether any game is inside its live window
	// (kicked off, not yet final) and only then refreshes that game's
	// week from CFBD and grades any games that just finished. See
	// internal/livepoll's package doc comment for the full design.
	poller := livepoll.NewPoller(scheduleService, gradingService)
	poller.Start(context.Background())

	// --- Notification dispatcher (Phase 7) ---
	//
	// A separate background ticker (every 20s by default) that drains
	// notification_outbox — see internal/notify's package doc comment for
	// the full design.
	dispatcher := notify.NewDispatcher(notifyService)
	dispatcher.Start(context.Background())

	server := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("survivor-league-api listening on %s (env=%s, cors_origin=%s)", server.Addr, appEnv, corsAllowedOrigin)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")

	shutdownCtx := cronScheduler.Stop() // stops the scheduler, returns a ctx that's Done once running jobs finish
	select {
	case <-shutdownCtx.Done():
	case <-time.After(30 * time.Second):
		log.Print("timed out waiting for in-flight cron jobs to finish")
	}

	poller.Stop()     // blocks until any in-flight tick finishes
	dispatcher.Stop() // blocks until any in-flight dispatch batch finishes

	httpShutdownCtx, cancelHTTPShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelHTTPShutdown()
	if err := server.Shutdown(httpShutdownCtx); err != nil {
		log.Printf("error during HTTP server shutdown: %v", err)
	}
}
