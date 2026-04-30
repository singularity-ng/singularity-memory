package store

import (
	"context"
	"fmt"
)

// EnsureBrainSchema installs the agent-brain relational surface inside the same
// Singularity Memory database. Memory rows remain the retrieval substrate;
// these tables preserve page/source/link/timeline/job semantics.
func (s *Store) EnsureBrainSchema(ctx context.Context) error {
	statements := []string{
		`ALTER TABLE ` + s.table("memory_units") + ` ADD COLUMN IF NOT EXISTS confidence_score double precision`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_sources") + ` (
			id text PRIMARY KEY,
			name text NOT NULL UNIQUE,
			local_path text,
			last_commit text,
			last_sync_at timestamptz,
			config jsonb NOT NULL DEFAULT '{}'::jsonb,
			chunker_version text,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`INSERT INTO ` + s.table("brain_sources") + ` (id, name, config)
		 VALUES ('default', 'default', '{"federated": true}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_pages") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			source_id text NOT NULL DEFAULT 'default' REFERENCES ` + s.table("brain_sources") + ` (id) ON DELETE CASCADE,
			slug text NOT NULL,
			type text NOT NULL DEFAULT 'note',
			page_kind text NOT NULL DEFAULT 'markdown' CHECK (page_kind IN ('markdown', 'code')),
			title text NOT NULL,
			compiled_truth text NOT NULL DEFAULT '',
			timeline text NOT NULL DEFAULT '',
			frontmatter jsonb NOT NULL DEFAULT '{}'::jsonb,
			content_hash text,
			document_id text NOT NULL,
			chunk_id text NOT NULL,
			memory_id uuid NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (bank_id, source_id, slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_pages_bank_updated ON ` + s.table("brain_pages") + ` (bank_id, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_pages_type ON ` + s.table("brain_pages") + ` (type)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_pages_frontmatter ON ` + s.table("brain_pages") + ` USING gin (frontmatter)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_links") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			source_id text NOT NULL DEFAULT 'default' REFERENCES ` + s.table("brain_sources") + ` (id) ON DELETE CASCADE,
			from_slug text NOT NULL,
			to_slug text NOT NULL,
			link_type text NOT NULL DEFAULT '',
			context text NOT NULL DEFAULT '',
			link_source text CHECK (link_source IS NULL OR link_source IN ('markdown', 'frontmatter', 'manual', 'agent')),
			origin_slug text,
			origin_field text,
			resolution_type text CHECK (resolution_type IS NULL OR resolution_type IN ('qualified', 'unqualified')),
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE NULLS NOT DISTINCT (bank_id, source_id, from_slug, to_slug, link_type, link_source, origin_slug)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_links_from ON ` + s.table("brain_links") + ` (bank_id, source_id, from_slug)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_links_to ON ` + s.table("brain_links") + ` (bank_id, source_id, to_slug)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_timeline_entries") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			source_id text NOT NULL DEFAULT 'default' REFERENCES ` + s.table("brain_sources") + ` (id) ON DELETE CASCADE,
			slug text NOT NULL,
			date text NOT NULL,
			source text NOT NULL DEFAULT '',
			summary text NOT NULL,
			detail text NOT NULL DEFAULT '',
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (bank_id, source_id, slug, date, summary)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_timeline_slug ON ` + s.table("brain_timeline_entries") + ` (bank_id, source_id, slug, date DESC)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_raw_data") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			source_id text NOT NULL DEFAULT 'default' REFERENCES ` + s.table("brain_sources") + ` (id) ON DELETE CASCADE,
			slug text NOT NULL,
			source text NOT NULL,
			data jsonb NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (bank_id, source_id, slug, source)
		)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_page_versions") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			source_id text NOT NULL DEFAULT 'default' REFERENCES ` + s.table("brain_sources") + ` (id) ON DELETE CASCADE,
			slug text NOT NULL,
			type text NOT NULL,
			title text NOT NULL,
			compiled_truth text NOT NULL,
			timeline text NOT NULL,
			frontmatter jsonb NOT NULL,
			content_hash text,
			created_at timestamptz NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_page_versions_slug ON ` + s.table("brain_page_versions") + ` (bank_id, source_id, slug, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS ` + s.table("brain_jobs") + ` (
			id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			bank_id text NOT NULL REFERENCES ` + s.table("banks") + ` (bank_id) ON DELETE CASCADE,
			kind text NOT NULL,
			status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled')),
			priority integer NOT NULL DEFAULT 100,
			params jsonb NOT NULL DEFAULT '{}'::jsonb,
			result jsonb NOT NULL DEFAULT '{}'::jsonb,
			error text,
			created_at timestamptz NOT NULL DEFAULT now(),
			updated_at timestamptz NOT NULL DEFAULT now(),
			started_at timestamptz,
			finished_at timestamptz
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_jobs_queue ON ` + s.table("brain_jobs") + ` (bank_id, status, priority, created_at)`,
	}
	for _, stmt := range statements {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure brain schema: %w", err)
		}
	}
	return nil
}
