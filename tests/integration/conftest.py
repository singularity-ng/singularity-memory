"""Pytest fixtures for integration tests."""

from __future__ import annotations

import os
import shutil
import socket
import subprocess
import time
import uuid
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
    """Start the Go server as a subprocess and yield its base URL."""
    env = os.environ.copy()
    env.setdefault(
        "SINGULARITY_DATABASE_URL",
        "postgresql://singularity_memory:password@127.0.0.1:5432/singularity_memory",
    )
    env.setdefault("SINGULARITY_DATABASE_SCHEMA", f"sm_contract_{uuid.uuid4().hex[:12]}")
    env.setdefault("SINGULARITY_MCP_ENABLED", "false")
    env.setdefault("SINGULARITY_FEATURE_BANKS", "true")
    env.setdefault("SINGULARITY_FEATURE_MEMORIES", "true")
    env.setdefault("SINGULARITY_STORAGE_PROFILE", "vchord")

    host = env.get("SINGULARITY_HOST", "127.0.0.1")
    port = int(env.get("SINGULARITY_PORT") or _pick_port())
    env["SINGULARITY_HOST"] = host
    env["SINGULARITY_PORT"] = str(port)
    url = f"http://{host}:{port}"

    binary = shutil.which("singularity-memory-go")
    if binary:
        command = [binary, "--host", host, "--port", str(port)]
        cwd = ROOT
    else:
        command = ["go", "run", "./cmd/singularity-memory-go", "--host", host, "--port", str(port)]
        cwd = ROOT / "go"

    proc = subprocess.Popen(
        command,
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )

    # Wait for /healthz to return 200 (with timeout)
    deadline = time.monotonic() + 30
    last_exc: Exception | None = None
    while time.monotonic() < deadline:
        try:
            resp = httpx.get(f"{url}/healthz", timeout=2)
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


@pytest.fixture()
def tmp_workspace() -> str:
    """Unique workspace per test to avoid cross-test pollution."""
    import uuid
    return f"test-{uuid.uuid4().hex[:8]}"
