// Command migrate is a thin wrapper around goose that runs the
// migrations/ directory against DATABASE_URL.
//
// Usage:
//
//	go run ./cmd/migrate up
//	go run ./cmd/migrate down
//	go run ./cmd/migrate status
//
// See https://github.com/pressly/goose for the full command list
// (up, up-by-one, down, down-to, redo, status, version, create, fix).
package main

import (
	"database/sql"
	"flag"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationsDir = "migrations"

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		log.Fatalf("usage: migrate <command> [args]  (e.g. migrate up, migrate status)")
	}
	command := args[0]

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("failed to set goose dialect: %v", err)
	}

	if err := goose.Run(command, db, migrationsDir, args[1:]...); err != nil {
		log.Fatalf("migrate %s: %v", command, err)
	}
}
