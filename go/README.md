# Singularity Memory Go

Greenfield Go port scaffold for the Singularity Memory server.

This module is the target runtime for the migration in `../MIGRATION.md`. The
old server code remains in the repo as contract/reference material while the Go
service takes over the HTTP, MCP, worker, and retrieval paths.

Current slice:

- `cmd/singularity-memory-go`: HTTP server binary.
- `GET /healthz`: service and Postgres connectivity health.
- `GET /v1/banks` and `GET /v1/default/banks`: first compatibility endpoint, backed by the
  existing `banks` table through `pgx`.
- Bank profile/create/update/delete endpoints under `/v1/default/banks`.
- Storage-profile-aware bank vector index creation: `vchord` creates
  `vchordrq (embedding vector_l2_ops)` partial indexes per bank and fact type.
- Native agent-brain layer:
  - `POST/GET /v1/default/banks/{bank_id}/brain/pages`
  - `GET /v1/default/banks/{bank_id}/brain/pages/{slug}`
  - `POST /v1/default/banks/{bank_id}/brain/links`
  - `GET /v1/default/banks/{bank_id}/brain/pages/{slug}/links`
  - `GET /v1/default/banks/{bank_id}/brain/pages/{slug}/backlinks`
  - `POST/GET /v1/default/banks/{bank_id}/brain/pages/{slug}/timeline`
  - `POST/GET /v1/default/banks/{bank_id}/brain/jobs`
  - `POST /v1/default/banks/{bank_id}/brain/jobs/claim`
  - `POST /v1/default/banks/{bank_id}/brain/jobs/{job_id}/complete`
- `cmd/import-brain`: imports external page/link/timeline data into a
  Singularity Memory bank while preserving page/source/link semantics.

Run locally:

```bash
cd go
SINGULARITY_DATABASE_URL=postgresql://singularity_memory:password@localhost:5432/singularity_memory \
SINGULARITY_STORAGE_PROFILE=vchord \
SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1 \
  go run ./cmd/singularity-memory-go
```

Verify:

```bash
curl -s http://127.0.0.1:8888/healthz | jq .
curl -s http://127.0.0.1:8888/v1/default/banks | jq .
curl -s http://127.0.0.1:8888/v1/default/banks/default/brain/pages | jq .
```

Model catalog:

```bash
# Refresh Catwalk + models.dev into the daemon cache.
curl -s -X POST http://127.0.0.1:8888/v1/model-catalog/sync | jq '.sources'

# Export the normalized overlay that SF can consume.
curl -s http://127.0.0.1:8888/v1/model-catalog/export/sf | jq '.policy'

# Local Charm TUI.
go run ./cmd/modelwalk --server http://127.0.0.1:8888

# Wish SSH TUI.
go run ./cmd/modelwalk --server http://127.0.0.1:8888 --ssh :23235 --host-key .modelwalk_ed25519
ssh localhost -p 23235
```

Live `/v1/models` checks are opt-in. The preferred SF path is to keep provider
discovery records in the SF SOPS namespace alongside the matching secret refs.
The daemon decrypts only `sf:` from `~/.dotfiles/secrets/api-keys.yaml` and
never returns key values in the HTTP API or TUI.

```yaml
sf:
  model_discovery:
    providers:
      zai:
        name: Z.AI
        base_url: https://api.z.ai/api/coding/paas/v4
        secret_ref: sf.env.ZAI_API_KEY
  env:
    ZAI_API_KEY: ...
```

The Go service accepts the core deployment environment variables for this
slice:

- `SINGULARITY_HOST`
- `SINGULARITY_PORT`
- `SINGULARITY_DATABASE_URL`
- `SINGULARITY_DATABASE_SCHEMA`
- `SINGULARITY_MCP_ENABLED`
- `SINGULARITY_STORAGE_PROFILE=vchord`
- `SINGULARITY_EMBEDDINGS_OPENAI_MODEL=qwen/qwen3-embedding-4b`
- `SINGULARITY_EMBEDDINGS_OPENAI_DIMENSIONS` unset for native 2560D; set only
  for explicit truncation experiments.
- `SINGULARITY_EMBEDDINGS_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
- `SINGULARITY_RERANK_OPENAI_BASE_URL=https://llm-gateway.centralcloud.com/v1`
- `SINGULARITY_MODEL_CATALOG_PATH=.singularity-memory/model-catalog.json`
- `SINGULARITY_MODEL_DISCOVERY_STORE_PATH=.singularity-memory/model-discovery.json`
- `SINGULARITY_MODEL_DISCOVERY_SECRET_SOURCE=env` or `sf-sops`; SOPS can also set this in the local discovery store.
- `SINGULARITY_MODEL_DISCOVERY_SOPS_FILE=~/.dotfiles/secrets/api-keys.yaml`
- `SINGULARITY_MODEL_DISCOVERY_SOPS_CONFIG=~/.dotfiles/.sops.yaml`
