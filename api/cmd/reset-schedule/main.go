// Command reset-schedule re-syncs the given season year from the real
// CFBD API, restoring every game's real kickoff_at/status/score — a
// one-off recovery tool for undoing cmd/seed-demo's fabricated game
// results (SyncSeason's upsert overwrites kickoff_at/status/scores from
// live CFBD data on every field except graded_at). Not wired into
// cmd/server or any CI job.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/schedule"
)

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
	}
	apiKey := os.Getenv("CFBD_API_KEY")
	if apiKey == "" {
		log.Fatal("CFBD_API_KEY must be set")
	}
	baseURL := os.Getenv("CFBD_BASE_URL")
	if baseURL == "" {
		baseURL = schedule.DefaultCFBDBaseURL
	}
	year := 2026
	if v := os.Getenv("SEASON_YEAR"); v != "" {
		y, err := strconv.Atoi(v)
		if err != nil {
			log.Fatalf("bad SEASON_YEAR: %v", err)
		}
		year = y
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	q := gen.New(pool)

	client := schedule.NewCFBDClient(http.DefaultClient, baseURL, apiKey)
	svc := schedule.NewService(q, client)

	result, err := svc.SyncSeason(ctx, year)
	if err != nil {
		log.Fatalf("sync: %v", err)
	}
	fmt.Printf("resynced %d: teams=%d weeks=%d games=%d skipped=%d pruned_empty_weeks=%d\n",
		year, result.TeamsUpserted, result.WeeksUpserted, result.GamesUpserted, result.GamesSkipped, result.WeeksPruned)
}
