#!/usr/bin/env bash
# Backfill NULL embeddings for memories stored during embedding service outages.
#
# Usage:
#   ./backfill-embeddings.sh <database_url> <bank_id> [batch_size]
#
# Example:
#   ./backfill-embeddings.sh \
#     "postgresql://ops_memory:ops_memory_2025@ops-memory-postgres.centralcloud-ops-memory.svc.cluster.local:5432/ops_memory" \
#     shared 100

set -euo pipefail

DB_URL="${1:-}"
BANK_ID="${2:-}"
BATCH_SIZE="${3:-100}"

if [[ -z "$DB_URL" || -z "$BANK_ID" ]]; then
  echo "Usage: $0 <database_url> <bank_id> [batch_size]"
  exit 1
fi

# This script is a placeholder — the actual backfill requires the embedding
# service to be available and either:
# 1. A Go binary with access to the embed client, or
# 2. Direct curl to the embedding endpoint + UPDATE queries
#
# For now, document the manual SQL approach:

cat <<'SQL'
-- Backfill NULL embeddings manually:
-- 1. Export memories needing embeddings:
--    COPY (
--      SELECT id, text || ' ' || COALESCE(context, '') AS content
--      FROM memory_units
--      WHERE bank_id = '<bank_id>' AND embedding IS NULL
--    ) TO '/tmp/needs_embed.csv' CSV;
--
-- 2. Send to embedding service (e.g. via curl), get vectors back.
--
-- 3. Update each row:
--    UPDATE memory_units SET embedding = '<vector>'::vector WHERE id = '<uuid>';
--
-- For automated backfill, build a small Go tool using the embed client.
SQL

echo "Manual backfill instructions printed above."
echo "Memories with NULL embedding in bank '$BANK_ID':"

psql "$DB_URL" -c "
  SELECT COUNT(*) AS null_embedding_count
  FROM memory_units
  WHERE bank_id = '$BANK_ID' AND embedding IS NULL;
"
