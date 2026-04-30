from __future__ import annotations

import uuid

import httpx


def test_retain_then_recall_against_postgres(http_client: httpx.Client) -> None:
    bank_id = f"ci-smoke-{uuid.uuid4().hex}"
    content = "Phase zero smoke memory: Qwen embeddings are batched through inference-fabric."

    try:
        create = http_client.put(
            f"/v1/default/banks/{bank_id}",
            json={
                "name": "CI smoke bank",
                "retain_extraction_mode": "chunks",
                "enable_observations": False,
            },
        )
        assert create.status_code == 200, create.text

        retain = http_client.post(
            f"/v1/default/banks/{bank_id}/memories",
            json={
                "items": [
                    {
                        "content": content,
                        "context": "CI integration smoke test",
                        "document_id": f"doc-{bank_id}",
                        "tags": ["ci-smoke"],
                    }
                ],
                "async": False,
            },
        )
        assert retain.status_code == 200, retain.text
        retain_body = retain.json()
        assert retain_body["success"] is True
        assert retain_body["bank_id"] == bank_id
        assert retain_body["items_count"] == 1
        assert retain_body["async"] is False

        recall = http_client.post(
            f"/v1/default/banks/{bank_id}/memories/recall",
            json={
                "query": "Where are Qwen embeddings batched?",
                "types": ["world", "experience"],
                "budget": "low",
                "max_tokens": 512,
                "trace": True,
                "include": {"entities": None, "chunks": {"max_tokens": 512}},
                "tags": ["ci-smoke"],
                "tags_match": "any_strict",
            },
        )
        assert recall.status_code == 200, recall.text
        recall_body = recall.json()
        assert "results" in recall_body
        assert isinstance(recall_body["results"], list)
        assert "trace" in recall_body
    finally:
        http_client.delete(f"/v1/default/banks/{bank_id}")
