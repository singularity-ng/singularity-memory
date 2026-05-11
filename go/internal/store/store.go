package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"

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
	// Set search_path so index names (which cannot be schema-qualified in
	// CREATE INDEX syntax) resolve to the correct schema automatically.
	// Include "public" for operator classes (vector_l2_ops etc.) and
	// "tokenizer_catalog" for the tokenize() function used by BM25 indexing.
	sep := "?"
	if strings.Contains(cfg.DatabaseURL, "?") {
		sep = "&"
	}
	dsn := cfg.DatabaseURL + sep + "search_path=" + cfg.DatabaseSchema + ",public,tokenizer_catalog"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	profile := cfg.StorageProfile
	if profile == "" {
		profile = storageprofile.VCHORD
	}
	s := &Store{pool: pool, schema: cfg.DatabaseSchema, storageProfile: profile}
	if err := s.EnsureBrainSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsRune(s, substr))
}

func containsRune(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
	// Index names cannot be schema-qualified in CREATE INDEX; the schema is set
	// via search_path on the connection so the index lands in the right schema.
	return `"` + name + `"`
}
