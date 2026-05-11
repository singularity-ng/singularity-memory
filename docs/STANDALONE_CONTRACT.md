# Standalone Provider Contract

This document formalizes the guarantees the Singularity Memory standalone server
makes to adapter clients (Hermes MemoryProvider, OpenClaw plugin) and to ACE
integrators. Enforced by `tests/integration/test_standalone_contract.py`.

---

## Contract Assertions

### Required Endpoints for Adapters

1. `GET /healthz` returns 2xx with JSON when the database is reachable.
2. `GET /v1/banks` returns 2xx with a JSON envelope containing `banks`.
3. `POST /v1/default/banks/{bank}/memories` accepts `{"items":[{"content":..., "context":...}]}` and returns a success envelope.
4. `POST /v1/default/banks/{bank}/memories/recall` accepts `{"query":..., "limit":N}` and returns `{"results":[...]}`.
5. `GET /v1/default/banks/{bank}/core-memory` returns core memory blocks.
6. `PUT /v1/default/banks/{bank}/core-memory/{block_name}` replaces a named block.
7. `POST /v1/default/banks/{bank}/consolidate` runs server-side consolidation when available.
8. `GET /v1/default/banks/{bank}/reflect` returns the agent-memory reflection view when available.

### Bank Isolation

9. Memories in `/v1/default/banks/workspace-a/...` are not returned by recall queries to `/v1/default/banks/workspace-b/...`.
10. Auth-per-workspace: the configured bearer token is forwarded as `Authorization: Bearer <token>` on all requests and reaches the server.

### ACE Non-Collapse Clause

11. ACE may vendor, embed, or wrap Singularity Memory but must not eliminate the standalone deployment path. All endpoints above must remain available to external adapters regardless of ACE deployment model.

---

## Canonical URL Pattern

```
/v1/default/banks/{bank}/memories
/v1/default/banks/{bank}/memories/recall
/v1/default/banks/{bank}/core-memory
/v1/default/banks/{bank}/core-memory/{block_name}
```

## Non-Goals

MCP JSON-RPC wire format, internal worker endpoints, admin shell, fantasy integration, multi-bank-per-workspace, entity graph traversal.
