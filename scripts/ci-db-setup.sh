#!/usr/bin/env bash
set -euo pipefail

# Database setup script for CI.
# Creates the pgvector extension, the target database, and runs Alembic migrations.

POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
POSTGRES_USER="${POSTGRES_USER:-postgres}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-postgres}"
POSTGRES_DB="${POSTGRES_DB:-postgres}"
SINGULARITY_DATABASE_URL="${SINGULARITY_DATABASE_URL:-postgresql://postgres:postgres@localhost:5432/singularity_memory_test}"

export PGPASSWORD="$POSTGRES_PASSWORD"

# Extract database name from DSN
db_name=$(echo "$SINGULARITY_DATABASE_URL" | sed -n 's|.*\/\([^/]*\)$|\1|p')

# 1. Create the vector extension in the template / default DB
psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "CREATE EXTENSION IF NOT EXISTS vector;" || true

# 2. Create the target database if it doesn't exist
psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tc "SELECT 1 FROM pg_database WHERE datname = '$db_name'" | grep -q 1 || \
    psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "CREATE DATABASE $db_name;"

# 3. Create vector extension in the target DB
psql -h "$POSTGRES_HOST" -p "$POSTGRES_PORT" -U "$POSTGRES_USER" -d "$db_name" -c "CREATE EXTENSION IF NOT EXISTS vector;" || true

# 4. Run Alembic migrations
SINGULARITY_RUN_MIGRATIONS_ON_STARTUP=false \
SINGULARITY_MCP_ENABLED=false \
SINGULARITY_LLM_PROVIDER=none \
SINGULARITY_EMBEDDINGS_PROVIDER=none \
SINGULARITY_RERANKER_PROVIDER=rrf \
    python -m alembic upgrade head

echo "Database setup complete: $SINGULARITY_DATABASE_URL"
