"""Standalone provider contract tests.

Exercises all adapter-required endpoints from docs/STANDALONE_CONTRACT.md.
These tests verify the Go-shaped HTTP contract that Hermes and OpenClaw
adapters depend on.
"""

from __future__ import annotations

import pytest

pytestmark = pytest.mark.integration


def test_healthz_returns_2xx(server_url: str, http_client: httpx.Client) -> None:
    """Assertion 1: GET /healthz returns 2xx when DB is reachable."""
    resp = http_client.get(f"{server_url}/healthz")
    assert resp.is_success, f"/healthz returned {resp.status_code}: {resp.text}"
    assert resp.status_code == 200
    body = resp.json()
    assert "status" in body or "ok" in body or "message" in body


def test_banks_endpoint_returns_json_array(server_url: str, http_client: httpx.Client) -> None:
    """Assertion 2: GET /v1/banks returns 2xx with a banks envelope."""
    resp = http_client.get(f"{server_url}/v1/banks")
    assert resp.is_success, f"/v1/banks returned {resp.status_code}: {resp.text}"
    data = resp.json()
    banks = data.get("banks") if isinstance(data, dict) else data
    assert isinstance(banks, list), f"Expected banks list, got {type(data)}"


def test_retain_accepts_content_and_context(
    server_url: str, http_client: httpx.Client, tmp_workspace: str
) -> None:
    """Assertion 3: POST /v1/default/banks/{bank}/memories works."""
    workspace = tmp_workspace
    resp = http_client.post(
        f"{server_url}/v1/default/banks/{workspace}/memories",
        json={"items": [{"content": "test fact for contract", "context": "contract test"}]},
    )
    assert resp.is_success, f"retain returned {resp.status_code}: {resp.text}"
    body = resp.json()
    assert body.get("success") or "memory_item_id" in body or "id" in body, f"Missing success/id field: {body}"


def test_recall_accepts_query_and_limit(
    server_url: str, http_client: httpx.Client, tmp_workspace: str
) -> None:
    """Assertion 4: POST /v1/default/banks/{bank}/memories/recall works."""
    workspace = tmp_workspace
    # First store something to recall
    http_client.post(
        f"{server_url}/v1/default/banks/{workspace}/memories",
        json={"items": [{"content": "contract recall test", "context": "test"}]},
    )
    resp = http_client.post(
        f"{server_url}/v1/default/banks/{workspace}/memories/recall",
        json={"query": "contract recall test", "limit": 5},
    )
    assert resp.is_success, f"recall returned {resp.status_code}: {resp.text}"
    body = resp.json()
    assert "results" in body, f"Missing results field: {body}"
    assert isinstance(body["results"], list)


def test_core_memory_get_returns_2xx(
    server_url: str, http_client: httpx.Client, tmp_workspace: str
) -> None:
    """Assertion 5: GET /v1/default/banks/{bank}/core-memory works."""
    workspace = tmp_workspace
    resp = http_client.get(f"{server_url}/v1/default/banks/{workspace}/core-memory")
    assert resp.is_success, f"core-memory get returned {resp.status_code}: {resp.text}"


def test_core_memory_set_replaces_block(
    server_url: str, http_client: httpx.Client, tmp_workspace: str
) -> None:
    """Assertion 6: PUT /v1/default/banks/{bank}/core-memory/{block} works."""
    workspace = tmp_workspace
    resp = http_client.put(
        f"{server_url}/v1/default/banks/{workspace}/core-memory/testblock",
        json={"content": "contract test block"},
    )
    assert resp.is_success, f"core-memory set returned {resp.status_code}: {resp.text}"


def test_bank_isolation_workspace_a_not_in_workspace_b(
    server_url: str, http_client: httpx.Client
) -> None:
    """Assertion 10: Bank isolation — workspace-a memories not in workspace-b."""
    ws_a = "contract-test-ws-a"
    ws_b = "contract-test-ws-b"
    # Store in ws_a
    http_client.post(
        f"{server_url}/v1/default/banks/{ws_a}/memories",
        json={"items": [{"content": "only in workspace A", "context": "test"}]},
    )
    # Recall from ws_b — should not contain ws_a memories
    resp = http_client.post(
        f"{server_url}/v1/default/banks/{ws_b}/memories/recall",
        json={"query": "only in workspace A", "limit": 5},
    )
    assert resp.is_success
    body = resp.json()
    results = body.get("results", [])
    for r in results:
        assert "only in workspace A" not in r.get("text", ""), (
            "Bank isolation violated: workspace A content leaked into workspace B"
        )
