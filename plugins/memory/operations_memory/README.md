# Operations Memory Hermes Provider

This is a Hermes Agent memory provider plugin. It follows Hermes' memory
provider layout:

```text
plugins/memory/operations_memory/
  __init__.py
  plugin.yaml
  README.md
```

It connects Hermes to a running Operations Memory server over HTTP.

## Configure

Use `hermes memory setup` and select `operations_memory`, or write
`$HERMES_HOME/operations-memory.json`:

```json
{
  "server_url": "http://127.0.0.1:8888",
  "bank_id": "default",
  "recall_mode": "hybrid",
  "context_tokens": 1200,
  "auto_sync": true
}
```

Then set the provider in Hermes:

```yaml
memory:
  provider: operations_memory
```

## Tools

- `operations_memory_context`: build a prompt context packet.
- `operations_memory_recall`: run hybrid recall.
- `operations_memory_remember`: store a memory.
- `operations_memory_core`: read or edit core memory blocks.

## Behavior

- `prefetch()` calls `/v1/default/banks/{bank_id}/context`.
- `sync_turn()` writes the completed turn in a daemon thread.
- `on_memory_write()` mirrors built-in Hermes memory writes into
  Operations Memory.
- `on_pre_compress()` stores a compression-safe summary candidate and returns
  a short preservation hint.

MCP remains the recommended integration path for non-Hermes clients.
