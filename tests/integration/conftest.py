"""Pytest fixtures for integration tests."""

from __future__ import annotations

import os
import subprocess
import sys
import time
from pathlib import Path

import httpx
import pytest

ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture(scope="session")
def server_url() -> str:
    """Start the Python server as a subprocess and yield its base URL."""
    env = os.environ.copy()
    env.setdefault(
        "SINGULARITY_DATABASE_URL",
        "postgresql://singularity:[REDACTED]@localhost:5432/singularity_memory",
    )
    env.setdefault("SINGULARITY_RUN_MIGRATIONS_ON_STARTUP", "false")
    env.setdefault("SINGULARITY_MCP_ENABLED", "false")
    env.setdefault("SINGULARITY_LLM_PROVIDER", "none")
    env.setdefault("SINGULARITY_EMBEDDINGS_PROVIDER", "none")
    env.setdefault("SINGULARITY_RERANKER_PROVIDER", "rrf")

    host = env.get("SINGULARITY_HOST", "127.0.0.1")
    port = int(env.get("SINGULARITY_PORT", "8888"))
    url = f"http://{host}:{port}"

    proc = subprocess.Popen(
        [sys.executable, "-m", "singularity_memory_server.main"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )

    # Wait for /health to return 200 (with timeout)
    deadline = time.monotonic() + 30
    last_exc: Exception | None = None
    while time.monotonic() < deadline:
        try:
            resp = httpx.get(f"{url}/health", timeout=2)
            if resp.status_code == 200:
                break
        except Exception as exc:
            last_exc = exc
        time.sleep(0.5)
    else:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        raise RuntimeError(
            f"Server did not become healthy within 30s (last error: {last_exc})"
        )

    try:
        yield url
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
