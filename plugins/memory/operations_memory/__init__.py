"""Operations Memory provider plugin for Hermes Agent."""

from __future__ import annotations

import json
import os
import threading
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any, Dict, List, Optional

try:
    from agent.memory_provider import MemoryProvider
except Exception:  # pragma: no cover - lets this file import outside Hermes.
    class MemoryProvider:  # type: ignore[no-redef]
        pass


DEFAULT_SERVER_URL = "http://127.0.0.1:8888"
DEFAULT_BANK_ID = "default"
DEFAULT_SHARED_BANK_ID = "shared"
DEFAULT_CONTEXT_TOKENS = 1200
DEFAULT_RECALL_MODE = "hybrid"
MAX_SYNC_CHARS = 25000

OPS_PRE_COMPRESS_PROMPT = (
    "Preserve: incident timeline, commands run and their outcomes, root cause identified, "
    "affected services and nodes, unresolved alerts, runbook gaps discovered, "
    "decisions made and why."
)


class OperationsMemoryProvider(MemoryProvider):
    """Hermes MemoryProvider implementation backed by Operations Memory."""

    def __init__(self) -> None:
        self._session_id = ""
        self._hermes_home = Path(os.environ.get("HERMES_HOME", "~/.hermes")).expanduser()
        self._server_url = DEFAULT_SERVER_URL
        self._bank_id = DEFAULT_BANK_ID
        self._shared_bank_id: Optional[str] = DEFAULT_SHARED_BANK_ID
        self._context_tokens = DEFAULT_CONTEXT_TOKENS
        self._recall_mode = DEFAULT_RECALL_MODE
        self._auto_sync = False
        self._timeout = 8.0
        self._sync_thread: Optional[threading.Thread] = None
        self._prefetch_cache: Dict[str, str] = {}

    @property
    def name(self) -> str:
        return "operations_memory"

    def is_available(self) -> bool:
        cfg = self._load_config()
        server_url = os.environ.get("OPS_MEMORY_URL") or cfg.get("server_url")
        return bool(server_url)

    def initialize(self, session_id: str, **kwargs: Any) -> None:
        self._session_id = session_id
        hermes_home = kwargs.get("hermes_home")
        if hermes_home:
            self._hermes_home = Path(str(hermes_home)).expanduser()
        cfg = self._load_config()
        self._server_url = str(os.environ.get("OPS_MEMORY_URL") or cfg.get("server_url") or DEFAULT_SERVER_URL).rstrip("/")
        self._bank_id = str(os.environ.get("OPS_MEMORY_BANK_ID") or cfg.get("bank_id") or DEFAULT_BANK_ID)
        shared_env = os.environ.get("OPS_MEMORY_SHARED_BANK_ID") or cfg.get("shared_bank_id") or DEFAULT_SHARED_BANK_ID
        self._shared_bank_id = str(shared_env) if shared_env else None
        self._context_tokens = int(cfg.get("context_tokens") or DEFAULT_CONTEXT_TOKENS)
        self._recall_mode = str(cfg.get("recall_mode") or DEFAULT_RECALL_MODE)
        auto_sync_env = os.environ.get("OPS_MEMORY_AUTO_SYNC")
        if auto_sync_env is not None:
            self._auto_sync = auto_sync_env.lower() not in ("0", "false", "no")
        else:
            self._auto_sync = bool(cfg.get("auto_sync", False))
        self._timeout = float(cfg.get("timeout_seconds") or 8.0)

    def system_prompt_block(self) -> str:
        return (
            "Operations Memory is active. Use operations_memory_recall for targeted recall, "
            "operations_memory_context for the current memory packet, and "
            "operations_memory_remember for durable facts worth retaining."
        )

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        key = session_id or self._session_id
        if key in self._prefetch_cache:
            return self._prefetch_cache.pop(key)
        return self._build_context(query, session_id=key)

    def queue_prefetch(self, query: str, *, session_id: str = "") -> None:
        key = session_id or self._session_id

        def _work() -> None:
            try:
                self._prefetch_cache[key] = self._build_context(query, session_id=key)
            except Exception:
                self._prefetch_cache[key] = ""

        threading.Thread(target=_work, daemon=True).start()

    def sync_turn(self, user_content: str, assistant_content: str, *, session_id: str = "") -> None:
        if not self._auto_sync:
            return

        def _sync() -> None:
            content = (
                f"User: {user_content[:MAX_SYNC_CHARS]}\n\n"
                f"Assistant: {assistant_content[:MAX_SYNC_CHARS]}"
            )
            self._retain(
                content,
                context="Hermes completed turn",
                tags=["hermes", "turn"],
                metadata={"session_id": session_id or self._session_id, "source": "hermes.sync_turn"},
            )

        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=1.0)
        self._sync_thread = threading.Thread(target=_sync, daemon=True)
        self._sync_thread.start()

    def on_pre_compress(self, messages: List[Dict[str, Any]]) -> str:
        text = _messages_to_text(messages)
        if not text:
            return ""
        self._retain(
            text[:MAX_SYNC_CHARS],
            context="Hermes pre-compression transcript",
            tags=["hermes", "pre_compress"],
            metadata={"session_id": self._session_id, "source": "hermes.on_pre_compress"},
        )
        return OPS_PRE_COMPRESS_PROMPT
    def on_memory_write(
        self,
        action: str,
        target: str,
        content: str,
        metadata: Optional[Dict[str, Any]] = None,
    ) -> None:
        if action not in {"add", "replace"} or not content:
            return
        md = dict(metadata or {})
        md.update({"target": target, "source": "hermes.on_memory_write", "action": action})
        self._retain(content, context=f"Hermes built-in {target} memory write", tags=["hermes", target], metadata=md)

    def shutdown(self) -> None:
        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=3.0)

    def get_config_schema(self) -> List[Dict[str, Any]]:
        return [
            {
                "key": "server_url",
                "description": "Operations Memory server URL",
                "default": DEFAULT_SERVER_URL,
                "required": True,
            },
            {
                "key": "bank_id",
                "description": "Operations Memory bank ID",
                "default": DEFAULT_BANK_ID,
            },
            {
                "key": "recall_mode",
                "description": "Memory flow mode",
                "default": DEFAULT_RECALL_MODE,
                "choices": ["hybrid", "context", "tools"],
            },
            {
                "key": "context_tokens",
                "description": "Maximum context packet tokens",
                "default": str(DEFAULT_CONTEXT_TOKENS),
            },
        ]

    def save_config(self, values: Dict[str, Any], hermes_home: str) -> None:
        path = Path(hermes_home).expanduser() / "operations-memory.json"
        config = {
            "server_url": values.get("server_url") or DEFAULT_SERVER_URL,
            "bank_id": values.get("bank_id") or DEFAULT_BANK_ID,
            "shared_bank_id": values.get("shared_bank_id") or DEFAULT_SHARED_BANK_ID,
            "recall_mode": values.get("recall_mode") or DEFAULT_RECALL_MODE,
            "context_tokens": int(values.get("context_tokens") or DEFAULT_CONTEXT_TOKENS),
            "auto_sync": False,
        }
        path.write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        if self._recall_mode == "context":
            return []
        return [
            {
                "name": "operations_memory_context",
                "description": "Build the current Operations Memory context packet.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string"},
                        "max_tokens": {"type": "integer", "default": self._context_tokens},
                    },
                },
            },
            {
                "name": "operations_memory_recall",
                "description": "Recall relevant memories with hybrid BM25/vector/graph retrieval.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string"},
                        "budget": {"type": "string", "enum": ["low", "mid", "high"], "default": "mid"},
                        "max_tokens": {"type": "integer"},
                    },
                    "required": ["query"],
                },
            },
            {
                "name": "operations_memory_remember",
                "description": "Store a durable memory in Operations Memory.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "content": {"type": "string"},
                        "context": {"type": "string"},
                        "tags": {"type": "array", "items": {"type": "string"}},
                    },
                    "required": ["content"],
                },
            },
            {
                "name": "operations_memory_core",
                "description": "Read or edit a core memory block.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "action": {"type": "string", "enum": ["list", "set", "append", "replace", "delete"]},
                        "block_name": {"type": "string"},
                        "content": {"type": "string"},
                        "text": {"type": "string"},
                        "old_text": {"type": "string"},
                        "new_text": {"type": "string"},
                        "char_limit": {"type": "integer"},
                        "description": {"type": "string"},
                    },
                    "required": ["action"],
                },
            },
        ]

    def handle_tool_call(self, tool_name: str, args: Dict[str, Any], **kwargs: Any) -> str:
        try:
            if tool_name == "operations_memory_context":
                return json.dumps({"context": self._build_context(str(args.get("query", "")), max_tokens=args.get("max_tokens"))})
            if tool_name == "operations_memory_recall":
                return json.dumps(self._recall(str(args["query"]), args))
            if tool_name == "operations_memory_remember":
                self._retain(
                    str(args["content"]),
                    context=str(args.get("context", "Hermes explicit memory")),
                    tags=list(args.get("tags") or ["hermes"]),
                    metadata={"session_id": kwargs.get("session_id") or self._session_id, "source": "hermes.tool"},
                )
                return json.dumps({"ok": True})
            if tool_name == "operations_memory_core":
                return json.dumps(self._core_memory(args))
        except Exception as exc:
            return json.dumps({"ok": False, "error": str(exc)})
        return json.dumps({"ok": False, "error": f"unknown tool {tool_name}"})

    def _load_config(self) -> Dict[str, Any]:
        path = self._hermes_home / "operations-memory.json"
        if not path.exists():
            return {}
        try:
            return json.loads(path.read_text(encoding="utf-8"))
        except Exception:
            return {}

    def _build_context(self, query: str, *, session_id: str = "", max_tokens: Any = None) -> str:
        token_budget = int(max_tokens or self._context_tokens)
        payload = {
            "query": query,
            "max_tokens": token_budget,
            "mode": self._recall_mode,
        }

        sections: list = []
        # Query private bank
        data = self._request("POST", f"/v1/default/banks/{self._bank_id}/context", payload)
        sections.extend(data.get("sections") or [])

        # Query shared bank if distinct from private bank
        if self._shared_bank_id and self._shared_bank_id != self._bank_id:
            shared_data = self._request("POST", f"/v1/default/banks/{self._shared_bank_id}/context", {
                "query": query,
                "max_tokens": max(token_budget // 2, 300),
                "mode": self._recall_mode,
            })
            for section in (shared_data.get("sections") or []):
                section = dict(section)
                section["name"] = f"shared:{section.get('name', 'memory')}"
                sections.append(section)

        if not sections:
            return ""
        body = "\n\n".join(f"[{s.get('name', 'memory')}]\n{s.get('text', '')}" for s in sections)
        return f"<operations-memory-context session_id=\"{session_id or self._session_id}\">\n{body}\n</operations-memory-context>"

    def _recall(self, query: str, args: Dict[str, Any]) -> Dict[str, Any]:
        payload = {
            "query": query,
            "budget": args.get("budget") or "mid",
            "max_tokens": args.get("max_tokens") or self._context_tokens,
            "include": {"entities": {"max_tokens": 120}, "chunks": {"max_tokens": 160}},
        }
        result = self._request("POST", f"/v1/default/banks/{self._bank_id}/memories/recall", payload)

        # Also query shared bank if distinct
        if self._shared_bank_id and self._shared_bank_id != self._bank_id:
            try:
                shared = self._request("POST", f"/v1/default/banks/{self._shared_bank_id}/memories/recall", payload)
                # Merge items from shared bank into result
                for key in ("chunks", "items", "results"):
                    if shared.get(key) and isinstance(shared[key], list):
                        result.setdefault(key, [])
                        result[key].extend(shared[key])
            except Exception:
                pass  # shared bank recall failure is non-fatal

        return result

    def _retain(self, content: str, *, context: str, tags: List[str], metadata: Dict[str, Any]) -> Dict[str, Any]:
        payload = {"items": [{"content": content, "context": context, "tags": tags, "metadata": metadata}]}
        return self._request("POST", f"/v1/default/banks/{self._bank_id}/memories", payload)

    def _core_memory(self, args: Dict[str, Any]) -> Dict[str, Any]:
        action = args.get("action")
        block_name = str(args.get("block_name") or "")
        base = f"/v1/default/banks/{self._bank_id}/core-memory"
        if action == "list":
            return self._request("GET", base)
        if not block_name:
            raise ValueError("block_name is required")
        if action == "set":
            return self._request("PUT", f"{base}/{block_name}", {
                "content": args.get("content") or "",
                "char_limit": args.get("char_limit") or 2000,
                "description": args.get("description") or "",
            })
        if action == "append":
            return self._request("PATCH", f"{base}/{block_name}/append", {"text": args.get("text") or args.get("content") or ""})
        if action == "replace":
            return self._request("PATCH", f"{base}/{block_name}/replace", {"old_text": args.get("old_text") or "", "new_text": args.get("new_text") or ""})
        if action == "delete":
            return self._request("DELETE", f"{base}/{block_name}")
        raise ValueError(f"unknown core action {action}")

    def _request(self, method: str, path: str, payload: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        body = None
        headers = {"Accept": "application/json"}
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self._server_url + path, data=body, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                raw = resp.read().decode("utf-8")
        except urllib.error.HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Operations Memory HTTP {exc.code}: {raw}") from exc
        if not raw.strip():
            return {}
        return json.loads(raw)


def _messages_to_text(messages: List[Dict[str, Any]]) -> str:
    parts: List[str] = []
    for message in messages:
        role = message.get("role") or message.get("name") or "message"
        content = message.get("content") or ""
        if isinstance(content, list):
            content = " ".join(str(part) for part in content)
        content = str(content).strip()
        if content:
            parts.append(f"{role}: {content}")
    return "\n\n".join(parts)


def register(ctx: Any) -> None:
    ctx.register_memory_provider(OperationsMemoryProvider())
