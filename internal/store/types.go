package store

import "time"

// MemoryUnit represents a single memory unit row in the database.
type MemoryUnit struct {
	ID                string         `json:"id"`
	BankID            string         `json:"bank_id"`
	DocumentID        *string        `json:"document_id,omitempty"`
	ChunkID           *string        `json:"chunk_id,omitempty"`
	Text              string         `json:"text"`
	Embedding         []float32      `json:"embedding,omitempty"`
	Context           *string        `json:"context,omitempty"`
	EventDate         time.Time      `json:"event_date"`
	OccurredStart     *time.Time     `json:"occurred_start,omitempty"`
	OccurredEnd       *time.Time     `json:"occurred_end,omitempty"`
	MentionedAt       *time.Time     `json:"mentioned_at,omitempty"`
	FactType          string         `json:"fact_type"`
	ConfidenceScore   *float64       `json:"confidence_score,omitempty"`
	AccessCount       int            `json:"access_count"`
	ProofCount        int            `json:"proof_count"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Tags              []string       `json:"tags,omitempty"`
	TextSignals       *string        `json:"text_signals,omitempty"`
	ObservationScopes []string       `json:"observation_scopes,omitempty"`
	SourceMemoryIDs   []string       `json:"source_memory_ids,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// MemoryLink represents a relationship between two memory units.
type MemoryLink struct {
	FromUnitID string    `json:"from_unit_id"`
	ToUnitID   string    `json:"to_unit_id"`
	LinkType   string    `json:"link_type"`
	EntityID   *string   `json:"entity_id,omitempty"`
	BankID     string    `json:"bank_id,omitempty"`
	Weight     float64   `json:"weight"`
	CreatedAt  time.Time `json:"created_at"`
}

// EntityObservation represents an observation about an entity.
type EntityObservation struct {
	Text        string    `json:"text"`
	MentionedAt time.Time `json:"mentioned_at"`
}

// Chunk represents a document chunk.
type Chunk struct {
	ChunkID     string    `json:"chunk_id"`
	DocumentID  string    `json:"document_id"`
	BankID      string    `json:"bank_id"`
	ChunkText   string    `json:"chunk_text"`
	ChunkIndex  int       `json:"chunk_index"`
	ContentHash *string   `json:"content_hash,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Entity represents an extracted entity.
type Entity struct {
	ID            string         `json:"id"`
	CanonicalName string         `json:"canonical_name"`
	BankID        string         `json:"bank_id"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	FirstSeen     time.Time      `json:"first_seen"`
	LastSeen      time.Time      `json:"last_seen"`
	MentionCount  int            `json:"mention_count"`
}
