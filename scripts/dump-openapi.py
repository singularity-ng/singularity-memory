#!/usr/bin/env python3
"""Dump the current Python FastAPI OpenAPI contract deterministically."""

from __future__ import annotations

import json
import os
from pathlib import Path


def main() -> int:
    os.environ.setdefault(
        "SINGULARITY_DATABASE_URL",
        "postgresql://singularity_memory:password@localhost:5432/singularity_memory",
    )
    os.environ.setdefault("SINGULARITY_RUN_MIGRATIONS_ON_STARTUP", "false")
    os.environ.setdefault("SINGULARITY_MCP_ENABLED", "false")
    os.environ.setdefault("SINGULARITY_LLM_PROVIDER", "none")
    os.environ.setdefault("SINGULARITY_EMBEDDINGS_PROVIDER", "none")
    os.environ.setdefault("SINGULARITY_RERANKER_PROVIDER", "rrf")

    from singularity_memory_server import MemoryEngine
    from singularity_memory_server.api import create_app

    memory = MemoryEngine(run_migrations=False)
    app = create_app(
        memory=memory,
        http_api_enabled=True,
        mcp_api_enabled=False,
        initialize_memory=False,
    )
    schema = app.openapi()

    out = Path(__file__).resolve().parents[1] / "openapi.json"
    out.write_text(json.dumps(schema, indent=2, sort_keys=True) + "\n")
    print(f"wrote {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
