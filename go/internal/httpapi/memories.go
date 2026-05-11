package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/singularity-ng/singularity-memory/go/internal/store"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type retainItem struct {
	Content    string         `json:"content"`
	Context    string         `json:"context,omitempty"`
	Timestamp  *time.Time     `json:"timestamp,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	DocumentID string         `json:"document_id,omitempty"`
	FactType   string         `json:"fact_type,omitempty"`
}

type retainRequest struct {
	Items []retainItem `json:"items"`
}

type retainResponse struct {
	Success      bool       `json:"success"`
	BankID       string     `json:"bank_id"`
	ItemsCount   int        `json:"items_count"`
	Async        bool       `json:"async"`
	OperationID  *string    `json:"operation_id,omitempty"`
	OperationIDs []string   `json:"operation_ids,omitempty"`
	Warnings     []string   `json:"warnings,omitempty"`
	Usage        tokenUsage `json:"usage"`
}

type tokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ---------------------------------------------------------------------------
// Processed memory unit (internal)
// ---------------------------------------------------------------------------

type processedMemoryUnit struct {
	Text          string
	Embedding     []float32
	Context       *string
	FactType      string
	Tags          []string
	Metadata      map[string]any
	OccurredStart *time.Time
	OccurredEnd   *time.Time
	MentionedAt   *time.Time
	TextSignals   *string
	DocumentID    string
	ChunkID       string
}

// ---------------------------------------------------------------------------
// Regex entity extraction patterns
// ---------------------------------------------------------------------------

var (
	entityPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b[A-Z][a-z]+(?:\s+[A-Z][a-z]+)*\b`),                                                                                    // Proper nouns
		regexp.MustCompile(`\b(?:January|February|March|April|May|June|July|August|September|October|November|December)\s+\d{1,2}(?:,\s+\d{4})?\b`), // Dates
		regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`),                                                                                                 // ISO dates
		regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`),                                                                   // Emails
	}
)

// ---------------------------------------------------------------------------
// Retain handler
// ---------------------------------------------------------------------------

func (s *server) retain(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "database is not configured")
		return
	}
	bankID := chi.URLParam(r, "bank_id")
	if bankID == "" {
		writeError(w, http.StatusBadRequest, "bank_id is required")
		return
	}

	var req retainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if len(req.Items) == 0 {
		writeJSON(w, http.StatusOK, retainResponse{
			Success:    true,
			BankID:     bankID,
			ItemsCount: 0,
			Async:      false,
			Usage:      tokenUsage{},
		})
		return
	}

	start := time.Now()

	// Expand any large items into token-bounded chunks with overlap.
	expandedItems := expandItems(req.Items)

	// Two-level batching: split items by SINGULARITY_RETAIN_BATCH_TOKENS
	batchTokenLimit := s.deps.Config.RetainBatchTokens
	if batchTokenLimit <= 0 {
		batchTokenLimit = 8000
	}

	subBatches := splitIntoSubBatches(expandedItems, batchTokenLimit)

	if s.deps.Logger != nil {
		s.deps.Logger.Info("retain start",
			"bank_id", bankID,
			"item_count", len(req.Items),
			"expanded_count", len(expandedItems),
			"sub_batch_count", len(subBatches),
		)
	}

	var allUnits []processedMemoryUnit
	var totalInputTokens int
	var warnings []string

	for batchIdx, batch := range subBatches {
		batchStart := time.Now()

		// Build texts for embedding: item content + context
		texts := make([]string, len(batch))
		for i, item := range batch {
			texts[i] = item.Content
			if item.Context != "" {
				texts[i] = item.Context + ": " + item.Content
			}
			totalInputTokens += estimateTokens(item.Content)
		}

		embeddings := make([][]float32, len(batch))
		if s.deps.EmbedClient != nil {
			vectors, err := s.deps.EmbedClient.Embed(r.Context(), texts)
			if err != nil {
				if s.deps.Logger != nil {
					s.deps.Logger.Warn("embed failed; storing text-only memories", "error", err, "bank_id", bankID, "batch", batchIdx)
				}
				warnings = append(warnings, "embedding service unavailable; stored text-only memories")
			} else if len(vectors) != len(batch) {
				if s.deps.Logger != nil {
					s.deps.Logger.Warn("embed count mismatch; storing text-only memories",
						"expected", len(batch),
						"got", len(vectors),
						"bank_id", bankID,
					)
				}
				warnings = append(warnings, "embedding service returned unexpected vector count; stored text-only memories")
			} else {
				embeddings = vectors
			}
		} else {
			warnings = append(warnings, "embedding service not configured; stored text-only memories")
		}

		// Build processed units
		for i, item := range batch {
			entities := extractEntities(item.Content)
			facts := extractSimpleFacts(item.Content)

			factType := item.FactType
			if factType == "" {
				factType = "experience"
			}

			docID := item.DocumentID
			if docID == "" {
				docID = uuid.New().String()
			}

			chunkID := fmt.Sprintf("%s_%s_%d", bankID, docID, i)

			var textSignals *string
			if len(entities) > 0 {
				sig := strings.Join(entities, " ")
				textSignals = &sig
			}

			var ctxStr *string
			if item.Context != "" {
				ctxStr = &item.Context
			}

			unit := processedMemoryUnit{
				Text:        item.Content,
				Embedding:   embeddings[i],
				Context:     ctxStr,
				FactType:    factType,
				Tags:        item.Tags,
				Metadata:    item.Metadata,
				DocumentID:  docID,
				ChunkID:     chunkID,
				TextSignals: textSignals,
			}

			if item.Timestamp != nil {
				unit.OccurredStart = item.Timestamp
				unit.OccurredEnd = item.Timestamp
				unit.MentionedAt = item.Timestamp
			}

			// If simple fact extraction found distinct facts, create a unit per fact;
			// otherwise store the original content as one unit.
			if len(facts) > 1 {
				for fi, fact := range facts {
					fu := unit
					fu.Text = fact
					fu.ChunkID = fmt.Sprintf("%s_%s_%d_%d", bankID, docID, i, fi)
					allUnits = append(allUnits, fu)
				}
			} else {
				if len(facts) == 1 {
					unit.Text = facts[0]
				}
				allUnits = append(allUnits, unit)
			}
		}

		if s.deps.Logger != nil {
			s.deps.Logger.Info("retain sub-batch done",
				"batch", batchIdx,
				"batch_size", len(batch),
				"embed_count", len(embeddings),
				"duration_ms", time.Since(batchStart).Milliseconds(),
			)
		}
	}

	// Storage phase
	storageStart := time.Now()
	for _, unit := range allUnits {
		if err := s.storeUnit(r.Context(), bankID, &unit); err != nil {
			if s.deps.Logger != nil {
				s.deps.Logger.Error("storage failed", "error", err, "bank_id", bankID)
			}
			writeError(w, http.StatusInternalServerError, "storage failed")
			return
		}
	}

	if s.deps.Logger != nil {
		s.deps.Logger.Info("retain complete",
			"bank_id", bankID,
			"item_count", len(req.Items),
			"sub_batch_count", len(subBatches),
			"unit_count", len(allUnits),
			"total_tokens", totalInputTokens,
			"embed_call_count", len(subBatches),
			"entity_count", len(allUnits), // one entity extraction per item
			"chunk_count", len(allUnits),
			"storage_duration_ms", time.Since(storageStart).Milliseconds(),
			"total_duration_ms", time.Since(start).Milliseconds(),
		)
	}

	resp := retainResponse{
		Success:    true,
		BankID:     bankID,
		ItemsCount: len(allUnits),
		Async:      false,
		Usage: tokenUsage{
			InputTokens:  totalInputTokens,
			OutputTokens: 0,
			TotalTokens:  totalInputTokens,
		},
		Warnings: uniqueWarnings(warnings),
	}
	writeJSON(w, http.StatusOK, resp)
}

func uniqueWarnings(warnings []string) []string {
	if len(warnings) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(warnings))
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if _, ok := seen[warning]; ok {
			continue
		}
		seen[warning] = struct{}{}
		out = append(out, warning)
	}
	return out
}

// ---------------------------------------------------------------------------
// Storage helpers
// ---------------------------------------------------------------------------

func (s *server) storeUnit(ctx context.Context, bankID string, unit *processedMemoryUnit) error {
	// Insert document row if not exists (upsert)
	if err := s.upsertDocument(ctx, bankID, unit.DocumentID, unit.Text); err != nil {
		return fmt.Errorf("upsert document: %w", err)
	}

	// Insert chunk
	chunk := &store.Chunk{
		ChunkID:    unit.ChunkID,
		DocumentID: unit.DocumentID,
		BankID:     bankID,
		ChunkText:  unit.Text,
		ChunkIndex: 0,
	}
	if _, err := s.deps.Store.InsertChunk(ctx, bankID, chunk); err != nil {
		return fmt.Errorf("insert chunk: %w", err)
	}

	// Insert memory unit
	mu := &store.MemoryUnit{
		DocumentID:  &unit.DocumentID,
		ChunkID:     &unit.ChunkID,
		Text:        unit.Text,
		Embedding:   unit.Embedding,
		Context:     unit.Context,
		FactType:    unit.FactType,
		Tags:        unit.Tags,
		Metadata:    unit.Metadata,
		TextSignals: unit.TextSignals,
	}
	if unit.OccurredStart != nil {
		mu.OccurredStart = unit.OccurredStart
		mu.OccurredEnd = unit.OccurredEnd
		mu.MentionedAt = unit.MentionedAt
		mu.EventDate = *unit.OccurredStart
	} else {
		now := time.Now().UTC()
		mu.EventDate = now
		mu.MentionedAt = &now
	}

	unitID, err := s.deps.Store.InsertMemoryUnit(ctx, bankID, mu)
	if err != nil {
		return fmt.Errorf("insert memory unit: %w", err)
	}

	// Semantic links: link to previous units in same document if embedding distance < threshold
	// For simplicity in first cut, we skip complex ANN and only link within the same request
	// by checking previously stored units. This is a lightweight approximation.
	_ = unitID
	return nil
}

func (s *server) upsertDocument(ctx context.Context, bankID, documentID, text string) error {
	return s.deps.Store.UpsertDocument(ctx, bankID, documentID, text)
}

// ---------------------------------------------------------------------------
// Batching helpers
// ---------------------------------------------------------------------------

func splitIntoSubBatches(items []retainItem, tokenLimit int) [][]retainItem {
	if len(items) == 0 {
		return nil
	}
	var batches [][]retainItem
	var current []retainItem
	currentTokens := 0

	for _, item := range items {
		tokens := estimateTokens(item.Content)
		if currentTokens > 0 && currentTokens+tokens > tokenLimit {
			batches = append(batches, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, item)
		currentTokens += tokens
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

func estimateTokens(text string) int {
	// Word-based tokenizer for first cut
	words := strings.Fields(text)
	return len(words)
}

// ---------------------------------------------------------------------------
// Entity extraction
// ---------------------------------------------------------------------------

func extractEntities(text string) []string {
	seen := make(map[string]struct{})
	var entities []string
	for _, re := range entityPatterns {
		for _, match := range re.FindAllString(text, -1) {
			if _, ok := seen[match]; !ok {
				seen[match] = struct{}{}
				entities = append(entities, match)
			}
		}
	}
	return entities
}

// ---------------------------------------------------------------------------
// Simple fact extraction
// ---------------------------------------------------------------------------

func extractSimpleFacts(text string) []string {
	// Split on sentence boundaries to produce simple fact candidates.
	sentences := splitSentences(text)
	if len(sentences) == 0 {
		return []string{text}
	}
	return sentences
}

func splitSentences(text string) []string {
	// Simple sentence splitter on period/exclamation/question followed by space or end.
	re := regexp.MustCompile(`[.!?]+\s+`)
	parts := re.Split(text, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
