package store

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/singularity-ng/singularity-memory/go/internal/config"
)

// testDB opens a real database connection using TEST_DATABASE_URL or
// SINGULARITY_DATABASE_URL.
// Tests are skipped when the env var is not set.
func testDB(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("SINGULARITY_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL/SINGULARITY_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := config.FromEnv()
	cfg.DatabaseURL = dsn
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		t.Fatalf("ping store: %v", err)
	}
	requireIntegrationPrereqs(t, ctx, db)
	return db
}

func requireIntegrationPrereqs(t *testing.T, ctx context.Context, db *Store) {
	t.Helper()

	var versionRaw string
	if err := db.pool.QueryRow(ctx, "SHOW server_version_num").Scan(&versionRaw); err != nil {
		db.Close()
		t.Skipf("cannot read postgres version: %v", err)
	}
	version, err := strconv.Atoi(versionRaw)
	if err != nil || version < 180000 {
		db.Close()
		t.Skipf("PG18+ is required for VectorChord integration tests, got server_version_num=%q", versionRaw)
	}

	requiredExtensions := []string{"vector", "vchord", "pg_tokenizer", "vchord_bm25", "pg_trgm"}
	for _, ext := range requiredExtensions {
		var installed bool
		err := db.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)", ext).Scan(&installed)
		if err != nil || !installed {
			db.Close()
			t.Skipf("required extension %s is not installed", ext)
		}
	}

	requiredTables := []string{"banks", "documents", "memory_units", "memory_links", "entities", "unit_entities", "chunks"}
	for _, table := range requiredTables {
		var exists bool
		regclass := db.schema + "." + table
		err := db.pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", regclass).Scan(&exists)
		if err != nil || !exists {
			db.Close()
			t.Skipf("required table %s is missing; run the Singularity Memory schema migrations first", regclass)
		}
	}
}

// cleanupBank removes all rows created for a test bank.
func cleanupBank(t *testing.T, db *Store, bankID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Order matters because of FK constraints.
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("memory_links")+` WHERE bank_id = $1`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("unit_entities")+` WHERE unit_id IN (SELECT id FROM `+db.table("memory_units")+` WHERE bank_id = $1)`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("memory_units")+` WHERE bank_id = $1`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("chunks")+` WHERE bank_id = $1`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("documents")+` WHERE bank_id = $1`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("entities")+` WHERE bank_id = $1`, bankID)
	_, _ = db.pool.Exec(ctx, `DELETE FROM `+db.table("banks")+` WHERE bank_id = $1`, bankID)
}

func TestIntegrationInsertMemoryUnit(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ensure bank exists.
	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	unit := &MemoryUnit{
		Text:      "The sky is blue",
		Context:   strPtr("weather observation"),
		FactType:  "world",
		Tags:      []string{"weather", "sky"},
		Metadata:  map[string]any{"source": "test"},
		EventDate: time.Now().UTC(),
	}

	id, err := db.InsertMemoryUnit(ctx, bankID, unit)
	if err != nil {
		t.Fatalf("insert memory unit: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty unit id")
	}

	// Verify round-trip.
	fetched, err := db.GetMemoryUnit(ctx, bankID, id)
	if err != nil {
		t.Fatalf("get memory unit: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected fetched unit, got nil")
	}
	if fetched.Text != unit.Text {
		t.Fatalf("text mismatch: got %q, want %q", fetched.Text, unit.Text)
	}
	if fetched.FactType != unit.FactType {
		t.Fatalf("fact_type mismatch: got %q, want %q", fetched.FactType, unit.FactType)
	}
	if fetched.Context == nil || *fetched.Context != *unit.Context {
		t.Fatalf("context mismatch: got %v, want %v", fetched.Context, unit.Context)
	}
}

func TestIntegrationInsertMemoryUnitWithVector(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) * 0.001
	}

	unit := &MemoryUnit{
		Text:      "Vector test content",
		FactType:  "world",
		Embedding: embedding,
		EventDate: time.Now().UTC(),
	}

	id, err := db.InsertMemoryUnit(ctx, bankID, unit)
	if err != nil {
		t.Fatalf("insert memory unit with vector: %v", err)
	}

	fetched, err := db.GetMemoryUnit(ctx, bankID, id)
	if err != nil {
		t.Fatalf("get memory unit: %v", err)
	}
	if fetched == nil {
		t.Fatal("expected fetched unit, got nil")
	}
	if len(fetched.Embedding) != len(embedding) {
		t.Fatalf("embedding length mismatch: got %d, want %d", len(fetched.Embedding), len(embedding))
	}
	for i := range embedding {
		if math.Abs(float64(fetched.Embedding[i]-embedding[i])) > 1e-4 {
			t.Fatalf("embedding mismatch at index %d: got %v, want %v", i, fetched.Embedding[i], embedding[i])
		}
	}
}

func TestIntegrationInsertMemoryLink(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	// Insert two memory units.
	unit1 := &MemoryUnit{Text: "Unit one", FactType: "world", EventDate: time.Now().UTC()}
	unit2 := &MemoryUnit{Text: "Unit two", FactType: "world", EventDate: time.Now().UTC()}

	id1, err := db.InsertMemoryUnit(ctx, bankID, unit1)
	if err != nil {
		t.Fatalf("insert unit1: %v", err)
	}
	id2, err := db.InsertMemoryUnit(ctx, bankID, unit2)
	if err != nil {
		t.Fatalf("insert unit2: %v", err)
	}

	link := &MemoryLink{
		FromUnitID: id1,
		ToUnitID:   id2,
		LinkType:   "semantic",
		Weight:     0.85,
	}
	if err := db.InsertMemoryLink(ctx, link); err != nil {
		t.Fatalf("insert memory link: %v", err)
	}

	// Verify link exists.
	var count int
	err = db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM `+db.table("memory_links")+`
		WHERE from_unit_id = $1 AND to_unit_id = $2 AND link_type = $3
	`, id1, id2, "semantic").Scan(&count)
	if err != nil {
		t.Fatalf("query link: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 link, got %d", count)
	}
}

func TestIntegrationListMemoryUnits(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	// Insert units with varied fact_types and tags.
	units := []*MemoryUnit{
		{Text: "World fact A", FactType: "world", Tags: []string{"alpha"}, EventDate: time.Now().UTC().Add(-2 * time.Hour)},
		{Text: "Experience B", FactType: "experience", Tags: []string{"alpha", "beta"}, EventDate: time.Now().UTC().Add(-1 * time.Hour)},
		{Text: "Observation C", FactType: "observation", Tags: []string{"beta"}, EventDate: time.Now().UTC()},
	}
	for _, u := range units {
		if _, err := db.InsertMemoryUnit(ctx, bankID, u); err != nil {
			t.Fatalf("insert unit: %v", err)
		}
	}

	// List all.
	all, err := db.ListMemoryUnits(ctx, bankID, 10, 0)
	if err != nil {
		t.Fatalf("list memory units: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 units, got %d", len(all))
	}

	// Pagination.
	page, err := db.ListMemoryUnits(ctx, bankID, 2, 0)
	if err != nil {
		t.Fatalf("list memory units page: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 units in page, got %d", len(page))
	}
}

func TestIntegrationGetEntityObservations(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	// Insert entity.
	entityID := uuid.New().String()
	_, err := db.pool.Exec(ctx, `
		INSERT INTO `+db.table("entities")+` (id, canonical_name, bank_id)
		VALUES ($1, $2, $3)
	`, entityID, "Alice", bankID)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	// Insert memory unit.
	unit := &MemoryUnit{
		Text:        "Alice visited Paris",
		FactType:    "observation",
		EventDate:   time.Now().UTC(),
		MentionedAt: timePtr(time.Now().UTC()),
	}
	unitID, err := db.InsertMemoryUnit(ctx, bankID, unit)
	if err != nil {
		t.Fatalf("insert memory unit: %v", err)
	}

	// Link unit to entity.
	_, err = db.pool.Exec(ctx, `
		INSERT INTO `+db.table("unit_entities")+` (unit_id, entity_id)
		VALUES ($1, $2)
	`, unitID, entityID)
	if err != nil {
		t.Fatalf("insert unit_entities: %v", err)
	}

	obs, err := db.GetEntityObservations(ctx, bankID, "Alice", 10)
	if err != nil {
		t.Fatalf("get entity observations: %v", err)
	}
	if len(obs) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(obs))
	}
	if obs[0].Text != unit.Text {
		t.Fatalf("observation text mismatch: got %q, want %q", obs[0].Text, unit.Text)
	}
}

func TestIntegrationBM25Tokenization(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	bankID := "test-bank-" + uuid.New().String()
	defer cleanupBank(t, db, bankID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := db.GetBank(ctx, bankID); err != nil {
		t.Fatalf("get bank: %v", err)
	}

	// Insert a unit with distinctive keywords.
	unit := &MemoryUnit{
		Text:      "The quick brown fox jumps over the lazy dog",
		FactType:  "world",
		EventDate: time.Now().UTC(),
	}
	if _, err := db.InsertMemoryUnit(ctx, bankID, unit); err != nil {
		t.Fatalf("insert memory unit: %v", err)
	}

	// Verify search_vector is populated by querying tsvector.
	var tsvec string
	err := db.pool.QueryRow(ctx, `
		SELECT search_vector::text
		FROM `+db.table("memory_units")+`
		WHERE bank_id = $1
		LIMIT 1
	`, bankID).Scan(&tsvec)
	if err != nil {
		t.Fatalf("query search_vector: %v", err)
	}
	if tsvec == "" {
		t.Fatal("expected non-empty search_vector")
	}
	// Distinctive keyword should be present in the tsvector.
	if !containsWord(tsvec, "fox") && !containsWord(tsvec, "quick") {
		t.Fatalf("search_vector missing expected tokens: %s", tsvec)
	}
}

func containsWord(tsvec, word string) bool {
	// tsvector format example: 'brown':3 'dog':9 'fox':4 'jump':5 'lazy':8 'quick':2
	return strings.Contains(tsvec, word)
}

func strPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
}
