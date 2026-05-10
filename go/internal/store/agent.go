package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
)

type CoreMemoryBlock struct {
	BankID      string    `json:"bank_id"`
	BlockName   string    `json:"block_name"`
	Content     string    `json:"content"`
	CharLimit   int       `json:"char_limit"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ConsolidationResult struct {
	BankID            string   `json:"bank_id"`
	SourceCount       int      `json:"source_count"`
	ObservationUnitID *string  `json:"observation_unit_id,omitempty"`
	Summary           string   `json:"summary"`
	SourceMemoryIDs   []string `json:"source_memory_ids,omitempty"`
}

type Reflection struct {
	BankID       string            `json:"bank_id"`
	CoreMemory   []CoreMemoryBlock `json:"core_memory"`
	Observations []MemoryUnit      `json:"observations"`
	Pages        []BrainPage       `json:"pages"`
}

func (s *Store) UpsertCoreMemoryBlock(ctx context.Context, block CoreMemoryBlock) (*CoreMemoryBlock, error) {
	if strings.TrimSpace(block.BankID) == "" {
		return nil, fmt.Errorf("bank_id is required")
	}
	if strings.TrimSpace(block.BlockName) == "" {
		return nil, fmt.Errorf("block_name is required")
	}
	if block.CharLimit <= 0 {
		block.CharLimit = 2000
	}
	if len(block.Content) > block.CharLimit {
		block.Content = block.Content[:block.CharLimit]
	}
	if _, err := s.GetBank(ctx, block.BankID); err != nil {
		return nil, err
	}
	var out CoreMemoryBlock
	err := s.pool.QueryRow(ctx, `
		INSERT INTO `+s.table("core_memory_blocks")+` (bank_id, block_name, content, char_limit, description)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (bank_id, block_name) DO UPDATE SET
			content = EXCLUDED.content,
			char_limit = EXCLUDED.char_limit,
			description = EXCLUDED.description,
			updated_at = now()
		RETURNING bank_id, block_name, content, char_limit, description, created_at, updated_at
	`, block.BankID, block.BlockName, block.Content, block.CharLimit, block.Description).Scan(
		&out.BankID, &out.BlockName, &out.Content, &out.CharLimit, &out.Description, &out.CreatedAt, &out.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert core memory block: %w", err)
	}
	return &out, nil
}

func (s *Store) ListCoreMemoryBlocks(ctx context.Context, bankID string) ([]CoreMemoryBlock, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bank_id, block_name, content, char_limit, description, created_at, updated_at
		FROM `+s.table("core_memory_blocks")+`
		WHERE bank_id = $1
		ORDER BY block_name
	`, bankID)
	if err != nil {
		return nil, fmt.Errorf("list core memory blocks: %w", err)
	}
	defer rows.Close()
	var blocks []CoreMemoryBlock
	for rows.Next() {
		var block CoreMemoryBlock
		if err := rows.Scan(&block.BankID, &block.BlockName, &block.Content, &block.CharLimit, &block.Description, &block.CreatedAt, &block.UpdatedAt); err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, rows.Err()
}

func (s *Store) RunDeterministicConsolidation(ctx context.Context, bankID string, limit int) (*ConsolidationResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, text
		FROM `+s.table("memory_units")+`
		WHERE bank_id = $1
			AND fact_type IN ('world', 'experience')
			AND consolidated_at IS NULL
		ORDER BY created_at ASC
		LIMIT $2
	`, bankID, limit)
	if err != nil {
		return nil, fmt.Errorf("load unconsolidated memories: %w", err)
	}
	defer rows.Close()
	var ids []string
	var lines []string
	for rows.Next() {
		var id, text string
		if err := rows.Scan(&id, &text); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		text = strings.TrimSpace(text)
		if text != "" {
			lines = append(lines, "- "+text)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := &ConsolidationResult{BankID: bankID, SourceCount: len(ids), SourceMemoryIDs: ids}
	if len(ids) == 0 {
		result.Summary = "No unconsolidated memories."
		return result, nil
	}
	summary := "Consolidated observations:\n" + strings.Join(lines, "\n")
	unitID, err := s.InsertMemoryUnit(ctx, bankID, &MemoryUnit{
		Text:      summary,
		FactType:  "observation",
		EventDate: time.Now().UTC(),
		Metadata: map[string]any{
			"agent_memory_kind": "consolidation",
			"source_count":      len(ids),
			"source_memory_ids": ids,
		},
		Tags:        []string{"consolidated", "agent-memory"},
		TextSignals: stringPtr("consolidated observation preference profile durable memory"),
	})
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE `+s.table("memory_units")+`
		SET consolidated_at = now()
		WHERE bank_id = $1 AND id::text = ANY($2)
	`, bankID, ids)
	if err != nil {
		return nil, fmt.Errorf("mark memories consolidated: %w", err)
	}
	nowCount := int64(0)
	_ = s.pool.QueryRow(ctx, `SELECT count(*) FROM `+s.table("memory_units")+` WHERE bank_id = $1`, bankID).Scan(&nowCount)
	_, _ = s.pool.Exec(ctx, `
		INSERT INTO `+s.table("consolidation_state")+` (bank_id, last_run_at, memories_at_last_run, updated_at)
		VALUES ($1, now(), $2, now())
		ON CONFLICT (bank_id) DO UPDATE SET
			last_run_at = EXCLUDED.last_run_at,
			memories_at_last_run = EXCLUDED.memories_at_last_run,
			updated_at = now()
	`, bankID, nowCount)
	result.ObservationUnitID = &unitID
	result.Summary = summary
	return result, nil
}

func (s *Store) ReflectAgentMemory(ctx context.Context, bankID string, limit int) (*Reflection, error) {
	if limit <= 0 {
		limit = 20
	}
	blocks, err := s.ListCoreMemoryBlocks(ctx, bankID)
	if err != nil {
		return nil, err
	}
	observations, err := s.listMemoryUnitsByType(ctx, bankID, "observation", limit)
	if err != nil {
		return nil, err
	}
	pages, err := s.ListBrainPages(ctx, bankID, limit)
	if err != nil {
		return nil, err
	}
	return &Reflection{BankID: bankID, CoreMemory: blocks, Observations: observations, Pages: pages}, nil
}

func (s *Store) listMemoryUnitsByType(ctx context.Context, bankID string, factType string, limit int) ([]MemoryUnit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, bank_id, document_id, text, context,
			event_date, occurred_start, occurred_end, mentioned_at,
			fact_type, confidence_score, access_count, metadata, tags,
			created_at, updated_at
		FROM `+s.table("memory_units")+`
		WHERE bank_id = $1 AND fact_type = $2
		ORDER BY event_date DESC NULLS LAST, created_at DESC
		LIMIT $3
	`, bankID, factType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var units []MemoryUnit
	for rows.Next() {
		var unit MemoryUnit
		var metadataBytes []byte
		if err := rows.Scan(
			&unit.ID, &unit.BankID, &unit.DocumentID, &unit.Text, &unit.Context,
			&unit.EventDate, &unit.OccurredStart, &unit.OccurredEnd, &unit.MentionedAt,
			&unit.FactType, &unit.ConfidenceScore, &unit.AccessCount, &metadataBytes, &unit.Tags,
			&unit.CreatedAt, &unit.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &unit.Metadata)
		}
		units = append(units, unit)
	}
	return units, rows.Err()
}

func stringPtr(s string) *string {
	return &s
}

func scanMemoryUnits(rows pgx.Rows) ([]MemoryUnit, error) {
	var units []MemoryUnit
	for rows.Next() {
		var unit MemoryUnit
		var metadataBytes []byte
		var embedding pgvector.Vector
		if err := rows.Scan(
			&unit.ID, &unit.BankID, &unit.DocumentID, &unit.Text, &embedding, &unit.Context,
			&unit.EventDate, &unit.OccurredStart, &unit.OccurredEnd, &unit.MentionedAt,
			&unit.FactType, &unit.ConfidenceScore, &unit.AccessCount, &metadataBytes, &unit.Tags,
			&unit.CreatedAt, &unit.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(embedding.Slice()) > 0 {
			unit.Embedding = append([]float32(nil), embedding.Slice()...)
		}
		if len(metadataBytes) > 0 {
			_ = json.Unmarshal(metadataBytes, &unit.Metadata)
		}
		units = append(units, unit)
	}
	return units, rows.Err()
}
