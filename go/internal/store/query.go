package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Query exposes the store connection for retrieval lanes that need custom SQL.
func (s *Store) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return s.pool.Query(ctx, sql, args...)
}

// QueryRow exposes the store connection for single-row custom retrieval SQL.
func (s *Store) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return s.pool.QueryRow(ctx, sql, args...)
}
