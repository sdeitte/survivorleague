package db

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// ParseUUID parses a string (e.g. a JWT subject claim or a URL path
// param) into a pgtype.UUID suitable for use as a sqlc query argument.
func ParseUUID(s string) (pgtype.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

// UUIDString renders a pgtype.UUID (as returned by sqlc-generated code)
// back into its canonical string form for JSON responses and JWT claims.
func UUIDString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}
