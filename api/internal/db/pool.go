package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a pgx connection pool for databaseURL. Callers are
// responsible for calling Close on the returned pool.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, databaseURL)
}
