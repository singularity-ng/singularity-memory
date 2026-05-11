# Hermes Integration

Operations Memory supports Hermes in two ways:

1. A native Hermes memory provider plugin at
   `plugins/memory/operations_memory/`.
2. The generic MCP server at `/mcp` for clients that do not use Hermes'
   MemoryProvider interface.

## Native Provider

Install by copying or symlinking the plugin directory into Hermes:

```bash
mkdir -p "$HERMES_HOME/plugins/memory"
ln -s /path/to/operations-memory/plugins/memory/operations_memory \
  "$HERMES_HOME/plugins/memory/operations_memory"
```

Configure Hermes:

```yaml
memory:
  provider: operations_memory
```

The setup wizard can write `$HERMES_HOME/operations-memory.json`. Minimal
manual config:

```json
{
  "server_url": "http://127.0.0.1:8888",
  "bank_id": "default",
  "recall_mode": "hybrid",
  "context_tokens": 1200,
  "auto_sync": true
}
```

Provider behavior:

- `prefetch()` injects an `<operations-memory-context>` block from
  `/v1/default/banks/{bank_id}/context`.
- `sync_turn()` stores completed turns in a daemon thread.
- `on_pre_compress()` preserves memory-relevant facts before Hermes compresses
  old context.
- `on_memory_write()` mirrors built-in memory writes to Operations Memory.
- Letta-style memory blocks are mapped to Operations Memory `core-memory`
  blocks. These are the small, editable, always-in-context facts that should
  stay stable across turns: persona, user profile, project constraints,
  preferences, and current operating rules.

Exposed tools:

- `operations_memory_context`
- `operations_memory_recall`
- `operations_memory_remember`
- `operations_memory_core`

## Memory Model

Operations Memory combines three memory shapes:

- Core blocks: Letta-style editable blocks, always eligible for context.
- Recall memories: Hindsight-style retained facts with VectorChord,
  VectorChord-BM25, entity links, graph expansion, and RRF fusion.
- Context packets: Honcho-style assembled context for Hermes, including core
  blocks, reflections, session-scoped material, and recall-ready summaries.

Hermes should use the native provider for lifecycle hooks and automatic
context. Other clients should use MCP.

## MCP

For Claude Code, Cursor, Windsurf, OpenCode, or custom agents, use the MCP
endpoint:

```bash
claude mcp add --transport http operations-memory http://localhost:8888/mcp/
```

MCP exposes bank/profile, retain, and recall tools without needing the Hermes
provider plugin.
