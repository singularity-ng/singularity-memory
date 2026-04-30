from __future__ import annotations

from types import SimpleNamespace

import pytest

from singularity_memory_server.engine.embeddings import OpenAIEmbeddings
from singularity_memory_server.engine.retain.embedding_utils import generate_embeddings_batch


class _FakeOpenAIEmbeddingsEndpoint:
    def __init__(self) -> None:
        self.calls: list[list[str]] = []

    def create(self, *, model: str, input: list[str]):
        self.calls.append(list(input))
        data = [
            SimpleNamespace(index=index, embedding=[float(ord(text[0]))])
            for index, text in enumerate(input)
        ]
        return SimpleNamespace(data=list(reversed(data)))


def test_openai_embeddings_batches_and_restores_response_order() -> None:
    endpoint = _FakeOpenAIEmbeddingsEndpoint()
    client = SimpleNamespace(embeddings=endpoint)

    embeddings = OpenAIEmbeddings(api_key="test", model="fake-embedding", batch_size=2)
    embeddings._client = client

    result = embeddings.encode(["a", "b", "c"])

    assert endpoint.calls == [["a", "b"], ["c"]]
    assert result == [[97.0], [98.0], [99.0]]


class _MismatchedBackend:
    def encode(self, texts: list[str]) -> list[list[float]]:
        return [[1.0] for _ in texts[:-1]]


@pytest.mark.asyncio
async def test_generate_embeddings_batch_rejects_vector_count_mismatch() -> None:
    with pytest.raises(RuntimeError, match="returned 2 vectors for 3 input texts"):
        await generate_embeddings_batch(_MismatchedBackend(), ["a", "b", "c"])
