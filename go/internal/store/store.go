package store

import (
	"context"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
)

var sqlIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Store struct {
	pool           *pgxpool.Pool
	schema         string
	storageProfile storageprofile.Profile
}

func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !sqlIdentifierRE.MatchString(cfg.DatabaseSchema) {
		return nil, fmt.Errorf("invalid database schema %q", cfg.DatabaseSchema)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	profile := cfg.StorageProfile
	if profile == "" {
		profile = storageprofile.VCHORD
	}
	return &Store{pool: pool, schema: cfg.DatabaseSchema, storageProfile: profile}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("database is not configured")
	}
	return s.pool.Ping(ctx)
}

func (s *Store) table(name string) string {
	if !sqlIdentifierRE.MatchString(name) {
		panic("invalid static SQL table name")
	}
	return `"` + s.schema + `"."` + name + `"`
}

func (s *Store) index(name string) string {
	if !sqlIdentifierRE.MatchString(name) {
		panic("invalid static SQL index name")
	}
	return `"` + s.schema + `"."` + name + `"`
}
