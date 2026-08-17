// Command buyback-once is a tiny local-dev CLI: buy back one membership
// by id, via the real leagues.Service.BuyBackMember (same code path the
// real API endpoint uses). Usage:
//
//	BUYBACK_LEAGUE_ID=... BUYBACK_MEMBERSHIP_ID=... BUYBACK_ACTOR_EMAIL=... go run ./cmd/buyback-once
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sdeitte/survivor-league-api/internal/db"
	"github.com/sdeitte/survivor-league-api/internal/db/gen"
	"github.com/sdeitte/survivor-league-api/internal/leagues"
)

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://survivor:survivor@localhost:5432/survivor_league?sslmode=disable"
	}
	leagueIDStr := os.Getenv("BUYBACK_LEAGUE_ID")
	membershipIDStr := os.Getenv("BUYBACK_MEMBERSHIP_ID")
	actorEmail := os.Getenv("BUYBACK_ACTOR_EMAIL")
	if leagueIDStr == "" || membershipIDStr == "" || actorEmail == "" {
		log.Fatal("BUYBACK_LEAGUE_ID, BUYBACK_MEMBERSHIP_ID, and BUYBACK_ACTOR_EMAIL must all be set")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	q := gen.New(pool)
	leaguesSvc := leagues.NewService(q, pool)

	leagueID, err := db.ParseUUID(leagueIDStr)
	if err != nil {
		log.Fatalf("parse league id: %v", err)
	}
	membershipID, err := db.ParseUUID(membershipIDStr)
	if err != nil {
		log.Fatalf("parse membership id: %v", err)
	}
	actor, err := q.GetUserByEmail(ctx, actorEmail)
	if err != nil {
		log.Fatalf("find actor user %s: %v", actorEmail, err)
	}

	m, err := leaguesSvc.BuyBackMember(ctx, leagueID, membershipID, actor.ID)
	if err != nil {
		log.Fatalf("buy back: %v", err)
	}
	fmt.Printf("bought back membership %s: status=%s bought_back=%v\n", db.UUIDString(m.ID), m.Status, m.BoughtBack)
}
