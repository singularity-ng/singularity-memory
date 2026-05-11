package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const brainPageKind = "page"

// BrainPage is an agent-readable page stored on top of the memory schema.
type BrainPage struct {
	Slug        string         `json:"slug"`
	SourceID    string         `json:"source_id"`
	Title       string         `json:"title"`
	Type        string         `json:"type,omitempty"`
	PageKind    string         `json:"page_kind"`
	Content     string         `json:"content"`
	Timeline    string         `json:"timeline,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Source      *string        `json:"source,omitempty"`
	DocumentID  string         `json:"document_id"`
	ChunkID     string         `json:"chunk_id"`
	MemoryID    string         `json:"memory_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// BrainPageInput is the write shape for an agent-readable page.
type BrainPageInput struct {
	Slug        string         `json:"slug"`
	SourceID    string         `json:"source_id,omitempty"`
	Title       string         `json:"title"`
	Type        string         `json:"type,omitempty"`
	PageKind    string         `json:"page_kind,omitempty"`
	Content     string         `json:"content"`
	Timeline    string         `json:"timeline,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Source      *string        `json:"source,omitempty"`
}

type BrainLink struct {
	FromSlug       string    `json:"from_slug"`
	ToSlug         string    `json:"to_slug"`
	LinkType       string    `json:"link_type"`
	Context        string    `json:"context"`
	LinkSource     *string   `json:"link_source,omitempty"`
	OriginSlug     *string   `json:"origin_slug,omitempty"`
	OriginField    *string   `json:"origin_field,omitempty"`
	ResolutionType *string   `json:"resolution_type,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type BrainTimelineEntry struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Date      string    `json:"date"`
	Source    string    `json:"source"`
	Summary   string    `json:"summary"`
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type BrainJob struct {
	ID         string         `json:"id"`
	BankID     string         `json:"bank_id"`
	Kind       string         `json:"kind"`
	Status     string         `json:"status"`
	Priority   int            `json:"priority"`
	Params     map[string]any `json:"params,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	Error      *string        `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

type BrainJobInput struct {
	Kind     string         `json:"kind"`
	Priority int            `json:"priority,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

func (s *Store) UpsertBrainPage(ctx context.Context, bankID string, input BrainPageInput) (*BrainPage, error) {
	slug := normalizeBrainSlug(input.Slug)
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, fmt.Errorf("content is required")
	}
	if _, err := s.GetBank(ctx, bankID); err != nil {
		return nil, fmt.Errorf("ensure bank: %w", err)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = slug
	}
	pageType := strings.TrimSpace(input.Type)
	if pageType == "" {
		pageType = "note"
	}
	pageKind := strings.TrimSpace(input.PageKind)
	if pageKind == "" {
		pageKind = "markdown"
	}
	sourceID := strings.TrimSpace(input.SourceID)
	if sourceID == "" {
		sourceID = "default"
	}

	documentID := "brain:" + sourceID + ":" + slug
	chunkID := "brain:" + bankID + ":" + sourceID + ":" + slug + ":0"
	memoryID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("operations-memory/brain/"+bankID+"/"+sourceID+"/"+slug)).String()
	now := time.Now().UTC()
	fullText := input.Content
	if strings.TrimSpace(input.Timeline) != "" {
		fullText += "\n\nTimeline:\n" + input.Timeline
	}

	metadata := map[string]any{
		"brain_kind":     brainPageKind,
		"brain_slug":     slug,
		"brain_source":   sourceID,
		"brain_title":    title,
		"brain_type":     pageType,
		"brain_pagekind": pageKind,
	}
	if input.Frontmatter != nil {
		metadata["frontmatter"] = input.Frontmatter
	}
	if input.Source != nil {
		metadata["source"] = *input.Source
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	_, err = tx.Exec(ctx, `
		INSERT INTO `+s.table("brain_sources")+` (id, name, config)
		VALUES ($1, $1, '{}'::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("ensure brain source: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO `+s.table("documents")+` (id, bank_id, original_text, retain_params, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $6)
		ON CONFLICT (id, bank_id) DO UPDATE SET
			original_text = EXCLUDED.original_text,
			retain_params = EXCLUDED.retain_params,
			tags = EXCLUDED.tags,
			updated_at = EXCLUDED.updated_at
	`, documentID, bankID, fullText, string(metadataJSON), input.Tags, now)
	if err != nil {
		return nil, fmt.Errorf("upsert brain document: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO `+s.table("chunks")+` (chunk_id, document_id, bank_id, chunk_text, chunk_index)
		VALUES ($1, $2, $3, $4, 0)
		ON CONFLICT (chunk_id) DO UPDATE SET
			chunk_text = EXCLUDED.chunk_text,
			chunk_index = EXCLUDED.chunk_index
	`, chunkID, documentID, bankID, fullText)
	if err != nil {
		return nil, fmt.Errorf("upsert brain chunk: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO `+s.table("brain_pages")+` (
			bank_id, source_id, slug, type, page_kind, title,
			compiled_truth, timeline, frontmatter, content_hash,
			document_id, chunk_id, memory_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9::jsonb, $10,
			$11, $12, $13, $14, $14
		)
		ON CONFLICT (bank_id, source_id, slug) DO UPDATE SET
			type = EXCLUDED.type,
			page_kind = EXCLUDED.page_kind,
			title = EXCLUDED.title,
			compiled_truth = EXCLUDED.compiled_truth,
			timeline = EXCLUDED.timeline,
			frontmatter = EXCLUDED.frontmatter,
			content_hash = EXCLUDED.content_hash,
			document_id = EXCLUDED.document_id,
			chunk_id = EXCLUDED.chunk_id,
			memory_id = EXCLUDED.memory_id,
			updated_at = EXCLUDED.updated_at
	`, bankID, sourceID, slug, pageType, pageKind, title, input.Content, input.Timeline, string(mustMarshalMap(input.Frontmatter)), nil, documentID, chunkID, memoryID, now)
	if err != nil {
		return nil, fmt.Errorf("upsert brain page: %w", err)
	}

	contextText := "brain page: " + title
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		INSERT INTO `+s.table("memory_units")+` (
			id, bank_id, document_id, text, context, event_date,
			occurred_start, occurred_end, mentioned_at, fact_type,
			metadata, tags, chunk_id, proof_count, text_signals, search_vector
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$6, $6, $6, 'world',
			$7::jsonb, $8, $9, 1, $10,
			tokenize(COALESCE($4, '') || ' ' || COALESCE($5, '') || ' ' || COALESCE($10, ''), 'llmlingua2')::bm25_catalog.bm25vector
		)
		ON CONFLICT (id) DO UPDATE SET
			text = EXCLUDED.text,
			context = EXCLUDED.context,
			event_date = EXCLUDED.event_date,
			occurred_start = EXCLUDED.occurred_start,
			occurred_end = EXCLUDED.occurred_end,
			mentioned_at = EXCLUDED.mentioned_at,
			metadata = EXCLUDED.metadata,
			tags = EXCLUDED.tags,
			chunk_id = EXCLUDED.chunk_id,
			text_signals = EXCLUDED.text_signals,
			search_vector = EXCLUDED.search_vector,
			updated_at = NOW()
		RETURNING created_at, updated_at
	`, memoryID, bankID, documentID, fullText, contextText, now, string(metadataJSON), input.Tags, chunkID, title+" "+slug+" "+sourceID).Scan(&createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert brain memory: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &BrainPage{
		Slug:        slug,
		SourceID:    sourceID,
		Title:       title,
		Type:        pageType,
		PageKind:    pageKind,
		Content:     input.Content,
		Timeline:    input.Timeline,
		Frontmatter: input.Frontmatter,
		Tags:        input.Tags,
		Source:      input.Source,
		DocumentID:  documentID,
		ChunkID:     chunkID,
		MemoryID:    memoryID,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func (s *Store) GetBrainPage(ctx context.Context, bankID string, slug string) (*BrainPage, error) {
	rows, err := s.queryBrainPages(ctx, bankID, normalizeBrainSlug(slug), 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (s *Store) ListBrainPages(ctx context.Context, bankID string, limit int) ([]BrainPage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	return s.queryBrainPages(ctx, bankID, "", limit)
}

func (s *Store) queryBrainPages(ctx context.Context, bankID string, slug string, limit int) ([]BrainPage, error) {
	args := []any{bankID, limit}
	whereSlug := ""
	if slug != "" {
		args = append(args, slug)
		whereSlug = "AND bp.slug = $3"
	}

	query := `
		SELECT
			bp.memory_id::text,
			bp.document_id,
			bp.chunk_id,
			bp.compiled_truth,
			bp.timeline,
			bp.source_id,
			bp.slug,
			bp.title,
			bp.type,
			bp.page_kind,
			bp.frontmatter,
			COALESCE(d.tags, '{}'::varchar[]),
			bp.created_at,
			bp.updated_at
		FROM ` + s.table("brain_pages") + ` bp
		LEFT JOIN ` + s.table("documents") + ` d ON d.id = bp.document_id AND d.bank_id = bp.bank_id
		WHERE bp.bank_id = $1
			` + whereSlug + `
		ORDER BY bp.updated_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query brain pages: %w", err)
	}
	defer rows.Close()

	var pages []BrainPage
	for rows.Next() {
		var page BrainPage
		var frontmatterBytes []byte
		if err := rows.Scan(
			&page.MemoryID,
			&page.DocumentID,
			&page.ChunkID,
			&page.Content,
			&page.Timeline,
			&page.SourceID,
			&page.Slug,
			&page.Title,
			&page.Type,
			&page.PageKind,
			&frontmatterBytes,
			&page.Tags,
			&page.CreatedAt,
			&page.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan brain page: %w", err)
		}
		if len(frontmatterBytes) > 0 {
			_ = json.Unmarshal(frontmatterBytes, &page.Frontmatter)
		}
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("brain page rows: %w", err)
	}
	return pages, nil
}

func (s *Store) AddBrainLink(ctx context.Context, bankID string, sourceID string, link BrainLink) error {
	sourceID = defaultBrainSource(sourceID)
	link.FromSlug = normalizeBrainSlug(link.FromSlug)
	link.ToSlug = normalizeBrainSlug(link.ToSlug)
	if link.FromSlug == "" {
		return fmt.Errorf("from_slug is required")
	}
	if link.ToSlug == "" {
		return fmt.Errorf("to_slug is required")
	}
	if _, err := s.GetBank(ctx, bankID); err != nil {
		return err
	}
	if err := s.ensureBrainSource(ctx, sourceID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO `+s.table("brain_links")+` (
			bank_id, source_id, from_slug, to_slug, link_type, context,
			link_source, origin_slug, origin_field, resolution_type
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (bank_id, source_id, from_slug, to_slug, link_type, link_source, origin_slug) DO UPDATE SET
			context = EXCLUDED.context,
			origin_field = EXCLUDED.origin_field,
			resolution_type = EXCLUDED.resolution_type
	`, bankID, sourceID, link.FromSlug, link.ToSlug, link.LinkType, link.Context, link.LinkSource, link.OriginSlug, link.OriginField, link.ResolutionType)
	if err != nil {
		return fmt.Errorf("add brain link: %w", err)
	}
	return nil
}

func (s *Store) GetBrainLinks(ctx context.Context, bankID string, sourceID string, slug string, backlinks bool) ([]BrainLink, error) {
	sourceID = defaultBrainSource(sourceID)
	column := "from_slug"
	if backlinks {
		column = "to_slug"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT from_slug, to_slug, link_type, context, link_source, origin_slug, origin_field, resolution_type, created_at
		FROM `+s.table("brain_links")+`
		WHERE bank_id = $1 AND source_id = $2 AND `+column+` = $3
		ORDER BY created_at DESC
	`, bankID, sourceID, normalizeBrainSlug(slug))
	if err != nil {
		return nil, fmt.Errorf("get brain links: %w", err)
	}
	defer rows.Close()
	var links []BrainLink
	for rows.Next() {
		var link BrainLink
		if err := rows.Scan(&link.FromSlug, &link.ToSlug, &link.LinkType, &link.Context, &link.LinkSource, &link.OriginSlug, &link.OriginField, &link.ResolutionType, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) AddBrainTimelineEntry(ctx context.Context, bankID string, sourceID string, entry BrainTimelineEntry) (*BrainTimelineEntry, error) {
	sourceID = defaultBrainSource(sourceID)
	entry.Slug = normalizeBrainSlug(entry.Slug)
	if entry.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	if strings.TrimSpace(entry.Date) == "" {
		return nil, fmt.Errorf("date is required")
	}
	if strings.TrimSpace(entry.Summary) == "" {
		return nil, fmt.Errorf("summary is required")
	}
	if _, err := s.GetBank(ctx, bankID); err != nil {
		return nil, err
	}
	if err := s.ensureBrainSource(ctx, sourceID); err != nil {
		return nil, err
	}
	var out BrainTimelineEntry
	err := s.pool.QueryRow(ctx, `
		INSERT INTO `+s.table("brain_timeline_entries")+` (bank_id, source_id, slug, date, source, summary, detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (bank_id, source_id, slug, date, summary) DO UPDATE SET
			source = EXCLUDED.source,
			detail = EXCLUDED.detail
		RETURNING id::text, slug, date, source, summary, detail, created_at
	`, bankID, sourceID, entry.Slug, entry.Date, entry.Source, entry.Summary, entry.Detail).Scan(
		&out.ID, &out.Slug, &out.Date, &out.Source, &out.Summary, &out.Detail, &out.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("add brain timeline: %w", err)
	}
	return &out, nil
}

func (s *Store) GetBrainTimeline(ctx context.Context, bankID string, sourceID string, slug string, limit int) ([]BrainTimelineEntry, error) {
	sourceID = defaultBrainSource(sourceID)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, slug, date, source, summary, detail, created_at
		FROM `+s.table("brain_timeline_entries")+`
		WHERE bank_id = $1 AND source_id = $2 AND slug = $3
		ORDER BY date DESC, created_at DESC
		LIMIT $4
	`, bankID, sourceID, normalizeBrainSlug(slug), limit)
	if err != nil {
		return nil, fmt.Errorf("get brain timeline: %w", err)
	}
	defer rows.Close()
	var entries []BrainTimelineEntry
	for rows.Next() {
		var entry BrainTimelineEntry
		if err := rows.Scan(&entry.ID, &entry.Slug, &entry.Date, &entry.Source, &entry.Summary, &entry.Detail, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) EnqueueBrainJob(ctx context.Context, bankID string, input BrainJobInput) (*BrainJob, error) {
	if _, err := s.GetBank(ctx, bankID); err != nil {
		return nil, err
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		return nil, fmt.Errorf("kind is required")
	}
	priority := input.Priority
	if priority <= 0 {
		priority = 100
	}

	var job BrainJob
	err := scanBrainJob(s.pool.QueryRow(ctx, `
		INSERT INTO `+s.table("brain_jobs")+` (bank_id, kind, priority, params)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id::text, bank_id, kind, status, priority, params, result, error, created_at, updated_at, started_at, finished_at
	`, bankID, kind, priority, string(mustMarshalMap(input.Params))), &job)
	if err != nil {
		return nil, fmt.Errorf("enqueue brain job: %w", err)
	}
	return &job, nil
}

func (s *Store) ListBrainJobs(ctx context.Context, bankID string, status string, limit int) ([]BrainJob, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{bankID, limit}
	statusClause := ""
	if strings.TrimSpace(status) != "" {
		args = append(args, strings.TrimSpace(status))
		statusClause = "AND status = $3"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, bank_id, kind, status, priority, params, result, error, created_at, updated_at, started_at, finished_at
		FROM `+s.table("brain_jobs")+`
		WHERE bank_id = $1 `+statusClause+`
		ORDER BY created_at DESC
		LIMIT $2
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list brain jobs: %w", err)
	}
	defer rows.Close()
	return scanBrainJobs(rows)
}

func (s *Store) ClaimBrainJob(ctx context.Context, bankID string, kinds []string) (*BrainJob, error) {
	var kindsArg any
	if len(kinds) > 0 {
		normalized := make([]string, 0, len(kinds))
		for _, kind := range kinds {
			kind = strings.TrimSpace(kind)
			if kind != "" {
				normalized = append(normalized, kind)
			}
		}
		if len(normalized) > 0 {
			kindsArg = normalized
		}
	}
	var job BrainJob
	err := scanBrainJob(s.pool.QueryRow(ctx, `
		WITH next_job AS (
			SELECT id
			FROM `+s.table("brain_jobs")+`
			WHERE bank_id = $1
				AND status = 'queued'
				AND ($2::text[] IS NULL OR kind = ANY($2::text[]))
			ORDER BY priority ASC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE `+s.table("brain_jobs")+` bj
		SET status = 'running',
			started_at = COALESCE(bj.started_at, now()),
			updated_at = now()
		FROM next_job
		WHERE bj.id = next_job.id
		RETURNING bj.id::text, bj.bank_id, bj.kind, bj.status, bj.priority, bj.params, bj.result, bj.error, bj.created_at, bj.updated_at, bj.started_at, bj.finished_at
	`, bankID, kindsArg), &job)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim brain job: %w", err)
	}
	return &job, nil
}

func (s *Store) CompleteBrainJob(ctx context.Context, bankID string, jobID string, status string, result map[string]any, jobErr *string) (*BrainJob, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		status = "succeeded"
	}
	if status != "succeeded" && status != "failed" && status != "canceled" {
		return nil, fmt.Errorf("status must be succeeded, failed, or canceled")
	}
	var job BrainJob
	err := scanBrainJob(s.pool.QueryRow(ctx, `
		UPDATE `+s.table("brain_jobs")+`
		SET status = $3,
			result = $4::jsonb,
			error = $5,
			finished_at = now(),
			updated_at = now()
		WHERE bank_id = $1 AND id = $2
		RETURNING id::text, bank_id, kind, status, priority, params, result, error, created_at, updated_at, started_at, finished_at
	`, bankID, jobID, status, string(mustMarshalMap(result)), jobErr), &job)
	if err != nil {
		return nil, fmt.Errorf("complete brain job: %w", err)
	}
	return &job, nil
}

func mustMarshalMap(v map[string]any) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func scanBrainJobs(rows pgx.Rows) ([]BrainJob, error) {
	var jobs []BrainJob
	for rows.Next() {
		var job BrainJob
		if err := scanBrainJob(rows, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

type brainJobScanner interface {
	Scan(dest ...any) error
}

func scanBrainJob(scanner brainJobScanner, job *BrainJob) error {
	var paramsBytes, resultBytes []byte
	if err := scanner.Scan(
		&job.ID,
		&job.BankID,
		&job.Kind,
		&job.Status,
		&job.Priority,
		&paramsBytes,
		&resultBytes,
		&job.Error,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.StartedAt,
		&job.FinishedAt,
	); err != nil {
		return err
	}
	if len(paramsBytes) > 0 {
		_ = json.Unmarshal(paramsBytes, &job.Params)
	}
	if len(resultBytes) > 0 {
		_ = json.Unmarshal(resultBytes, &job.Result)
	}
	return nil
}

func defaultBrainSource(sourceID string) string {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return "default"
	}
	return sourceID
}

func (s *Store) ensureBrainSource(ctx context.Context, sourceID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO `+s.table("brain_sources")+` (id, name, config)
		VALUES ($1, $1, '{}'::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, defaultBrainSource(sourceID))
	if err != nil {
		return fmt.Errorf("ensure brain source: %w", err)
	}
	return nil
}

func applyBrainMetadata(page *BrainPage, metadataBytes []byte) {
	var metadata map[string]any
	if len(metadataBytes) == 0 || json.Unmarshal(metadataBytes, &metadata) != nil {
		return
	}
	if slug, ok := metadata["brain_slug"].(string); ok {
		page.Slug = slug
	}
	if title, ok := metadata["brain_title"].(string); ok {
		page.Title = title
	}
	if pageType, ok := metadata["brain_type"].(string); ok {
		page.Type = pageType
	}
	if frontmatter, ok := metadata["frontmatter"].(map[string]any); ok {
		page.Frontmatter = frontmatter
	}
	if source, ok := metadata["source"].(string); ok {
		page.Source = &source
	}
}

func normalizeBrainSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	slug = strings.Trim(slug, "/")
	return slug
}
