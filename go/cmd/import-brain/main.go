package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/singularity-ng/singularity-memory/go/internal/config"
	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "import-brain failed: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	sourceURL := fs.String("source-db", os.Getenv("BRAIN_SOURCE_DATABASE_URL"), "source brain Postgres URL")
	bankID := fs.String("bank", "default", "target Singularity Memory bank")
	limit := fs.Int("limit", 0, "optional max pages to import")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *sourceURL == "" {
		return fmt.Errorf("-source-db or BRAIN_SOURCE_DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cfg := config.FromEnv()
	target, err := store.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer target.Close()

	source, err := pgxpool.New(ctx, *sourceURL)
	if err != nil {
		return err
	}
	defer source.Close()

	importedPages, err := importPages(ctx, source, target, *bankID, *limit)
	if err != nil {
		return err
	}
	importedLinks, err := importLinks(ctx, source, target, *bankID)
	if err != nil {
		return err
	}
	importedTimeline, err := importTimeline(ctx, source, target, *bankID)
	if err != nil {
		return err
	}

	fmt.Printf("imported pages=%d links=%d timeline=%d bank=%s\n", importedPages, importedLinks, importedTimeline, *bankID)
	return nil
}

func importPages(ctx context.Context, source *pgxpool.Pool, target *store.Store, bankID string, limit int) (int, error) {
	query := `
		SELECT source_id, slug, type, page_kind, title, compiled_truth, timeline, frontmatter
		FROM pages
		ORDER BY updated_at ASC
	`
	args := []any{}
	if limit > 0 {
		query += " LIMIT $1"
		args = append(args, limit)
	}
	rows, err := source.Query(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var sourceID, slug, pageType, pageKind, title, content, timeline string
		var frontmatterBytes []byte
		if err := rows.Scan(&sourceID, &slug, &pageType, &pageKind, &title, &content, &timeline, &frontmatterBytes); err != nil {
			return count, err
		}
		var frontmatter map[string]any
		if len(frontmatterBytes) > 0 {
			_ = json.Unmarshal(frontmatterBytes, &frontmatter)
		}
		if _, err := target.UpsertBrainPage(ctx, bankID, store.BrainPageInput{
			Slug:        slug,
			SourceID:    sourceID,
			Title:       title,
			Type:        pageType,
			PageKind:    pageKind,
			Content:     content,
			Timeline:    timeline,
			Frontmatter: frontmatter,
		}); err != nil {
			return count, fmt.Errorf("import page %s:%s: %w", sourceID, slug, err)
		}
		count++
	}
	return count, rows.Err()
}

func importLinks(ctx context.Context, source *pgxpool.Pool, target *store.Store, bankID string) (int, error) {
	rows, err := source.Query(ctx, `
		SELECT
			COALESCE(p_from.source_id, 'default') AS source_id,
			p_from.slug,
			p_to.slug,
			l.link_type,
			l.context,
			l.link_source,
			p_origin.slug,
			l.origin_field,
			l.resolution_type
		FROM links l
		JOIN pages p_from ON p_from.id = l.from_page_id
		JOIN pages p_to ON p_to.id = l.to_page_id
		LEFT JOIN pages p_origin ON p_origin.id = l.origin_page_id
		ORDER BY l.created_at ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var sourceID string
		var link store.BrainLink
		if err := rows.Scan(&sourceID, &link.FromSlug, &link.ToSlug, &link.LinkType, &link.Context, &link.LinkSource, &link.OriginSlug, &link.OriginField, &link.ResolutionType); err != nil {
			return count, err
		}
		if err := target.AddBrainLink(ctx, bankID, sourceID, link); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

func importTimeline(ctx context.Context, source *pgxpool.Pool, target *store.Store, bankID string) (int, error) {
	rows, err := source.Query(ctx, `
		SELECT COALESCE(p.source_id, 'default'), p.slug, te.date::text, te.source, te.summary, te.detail
		FROM timeline_entries te
		JOIN pages p ON p.id = te.page_id
		ORDER BY te.created_at ASC
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var sourceID string
		var entry store.BrainTimelineEntry
		if err := rows.Scan(&sourceID, &entry.Slug, &entry.Date, &entry.Source, &entry.Summary, &entry.Detail); err != nil {
			return count, err
		}
		if _, err := target.AddBrainTimelineEntry(ctx, bankID, sourceID, entry); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}
