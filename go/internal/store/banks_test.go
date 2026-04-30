package store

import (
	"strings"
	"testing"

	"github.com/singularity-ng/singularity-memory/go/internal/storageprofile"
)

const testBankInternalID = "12345678-1234-1234-1234-123456789abc"

func TestBankVectorIndexSQLUsesVectorChordForDefaultProfile(t *testing.T) {
	db := &Store{schema: "memory"}

	query := db.bankVectorIndexSQL("agent's bank", testBankInternalID, bankIndexFactTypes[0])

	assertContains(t, query, `CREATE INDEX IF NOT EXISTS "memory"."idx_mu_emb_worl_1234567812341234"`)
	assertContains(t, query, `ON "memory"."memory_units" USING vchordrq (embedding vector_l2_ops)`)
	assertContains(t, query, `WHERE fact_type = 'world' AND bank_id = 'agent''s bank'`)
}

func TestBankVectorIndexSQLUsesVectorChordForVChordProfile(t *testing.T) {
	db := &Store{schema: "memory", storageProfile: storageprofile.VCHORD}

	query := db.bankVectorIndexSQL("default", testBankInternalID, bankIndexFactTypes[1])

	assertContains(t, query, `USING vchordrq (embedding vector_l2_ops)`)
	assertContains(t, query, `fact_type = 'experience'`)
}

func TestBankVectorIndexSQLUsesHNSWForPGVectorProfile(t *testing.T) {
	db := &Store{schema: "memory", storageProfile: storageprofile.PGVECTOR}

	query := db.bankVectorIndexSQL("default", testBankInternalID, bankIndexFactTypes[2])

	assertContains(t, query, `USING hnsw (embedding vector_cosine_ops)`)
	assertContains(t, query, `fact_type = 'observation'`)
	if strings.Contains(query, "vchordrq") {
		t.Fatalf("pgvector index SQL should not use vchordrq: %s", query)
	}
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("query %q does not contain %q", got, want)
	}
}
