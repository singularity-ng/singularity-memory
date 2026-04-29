"""Pytest fixtures for integration tests."""

from __future__ import annotations

import os
import socket
import subprocess
import sys
import time
from pathlib import Path

import httpx
import pytest

ROOT = Path(__file__).resolve().parents[2]


def _pick_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def _stop_server(proc: subprocess.Popen[str]) -> tuple[str, str]:
    if proc.poll() is None:
        proc.terminate()
        try:
            return proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
    return proc.communicate(timeout=5)


@pytest.fixture(scope="session")
def server_url() -> str:
    """Start the Python server as a subprocess and yield its base URL."""
    env = os.environ.copy()
    env.setdefault(
        "SINGULARITY_DATABASE_URL",
        "postgresql://singularity:singularity@127.0.0.1:5432/singularity_memory",
    )
    env.setdefault("SINGULARITY_RUN_MIGRATIONS_ON_STARTUP", "false")
    env.setdefault("SINGULARITY_MCP_ENABLED", "false")
    env.setdefault("SINGULARITY_LLM_PROVIDER", "none")
    env.setdefault("SINGULARITY_EMBEDDINGS_PROVIDER", "none")
    env.setdefault("SINGULARITY_RERANKER_PROVIDER", "rrf")
    env.setdefault("SINGULARITY_VECTOR_ENABLED", "false")
    env.setdefault("SINGULARITY_ENABLE_OBSERVATIONS", "false")
    env.setdefault("SINGULARITY_RETAIN_BATCH_ENABLED", "false")
    env.setdefault("SINGULARITY_LOG_LEVEL", "warning")

    host = env.get("SINGULARITY_HOST", "127.0.0.1")
    port = int(env.get("SINGULARITY_PORT") or _pick_port())
    env["SINGULARITY_HOST"] = host
    env["SINGULARITY_PORT"] = str(port)
    url = f"http://{host}:{port}"

    proc = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "singularity_memory_server.main",
            "--host",
            host,
            "--port",
            str(port),
            "--log-level",
            env["SINGULARITY_LOG_LEVEL"],
        ],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
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
        if proc.poll() is not None:
            stdout, stderr = proc.communicate(timeout=5)
            raise RuntimeError(
                "Server exited before becoming healthy.\n"
                f"stdout:\n{stdout[-4000:]}\n"
                f"stderr:\n{stderr[-4000:]}"
            )
        time.sleep(0.5)
    else:
        stdout, stderr = _stop_server(proc)
        raise RuntimeError(
            "Server did not become healthy within 30s "
            f"(last error: {last_exc}).\nstdout:\n{stdout[-4000:]}\nstderr:\n{stderr[-4000:]}"
        )

    try:
        yield url
    finally:
        _stop_server(proc)


@pytest.fixture()
def http_client(server_url: str) -> httpx.Client:
    with httpx.Client(base_url=server_url, timeout=30) as client:
        yield client
