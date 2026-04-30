from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def test_committed_openapi_matches_python_contract(tmp_path: Path) -> None:
    committed = ROOT / "openapi.json"
    assert committed.exists(), "run scripts/dump-openapi.py and commit openapi.json"

    env = os.environ.copy()
    env.setdefault(
        "SINGULARITY_DATABASE_URL",
        "postgresql://singularity_memory:password@localhost:5432/singularity_memory",
    )
    env.setdefault("SINGULARITY_RUN_MIGRATIONS_ON_STARTUP", "false")
    env.setdefault("SINGULARITY_MCP_ENABLED", "false")

    before = committed.read_text()
    try:
        subprocess.run(
            [sys.executable, str(ROOT / "scripts" / "dump-openapi.py")],
            cwd=ROOT,
            env=env,
            check=True,
        )
        regenerated = committed.read_text()
    finally:
        committed.write_text(before)

    assert json.loads(regenerated) == json.loads(before)
