from __future__ import annotations

import httpx


def test_go_health_endpoint_is_ready(http_client: httpx.Client) -> None:
	response = http_client.get("/health")

	assert response.status_code == 200
	body = response.json()
	assert body["status"] == "healthy"
	assert body["service"] == "singularity-memory-go"
