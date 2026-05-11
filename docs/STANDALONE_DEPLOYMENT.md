# Standalone Deployment

Singularity Memory is a freestanding MCP+HTTP memory provider. ACE can vendor
or wrap it, but Hermes and OpenClaw must be able to use it without adopting
ACE.

## Priority 1: Hermes

Use this path first for a personal Hermes agent, including a hosted setup such
as `portal.hugo.dk`.

1. Run the server:

   ```bash
   docker compose up -d singularity-memory-postgres singularity-memory
   ```

2. Verify health from the Hermes host:

   ```bash
   curl -fsS http://127.0.0.1:8888/healthz
   curl -fsS http://127.0.0.1:8888/v1/banks
   ```

3. Install the Hermes adapter by linking or copying `extensions/hermes` into
   the Hermes plugin directory:

   ```bash
   mkdir -p "$HERMES_HOME/plugins"
   ln -s /path/to/singularity-memory/extensions/hermes \
     "$HERMES_HOME/plugins/singularity_memory"
   ```

4. Configure Hermes:

   ```json
   {
     "server_url": "https://portal.hugo.dk/memory",
     "workspace": "personal-hermes",
     "server_api_key": "optional-bearer-token"
   }
   ```

   Save this as `$HERMES_HOME/singularity-memory.json`.

5. Confirm the adapter exposes these tools:

   - `singularity_memory_search`
   - `singularity_memory_context`
   - `singularity_memory_store`
   - `singularity_memory_status`
   - `singularity_core_memory_get`
   - `singularity_core_memory_set`
   - `singularity_core_memory_append`
   - `singularity_core_memory_replace`
   - `singularity_memory_summarize_offload`

The Hermes adapter fences recalled context in `<singularity-memory-context>`
and core memory in `<singularity-core-memory>`. Retrieved memory is background
data, not instructions.

## Portal/Tailnet Shape

For a personal hosted deployment:

- Bind the server privately by default.
- Put TLS/auth at the portal or reverse-proxy layer.
- Use a single personal workspace such as `personal-hermes`.
- Keep Postgres on the same host or private network.
- Expose `/healthz`, `/v1/banks`, and `/mcp/` only to trusted clients.
- Rotate bearer tokens if a laptop or agent host is lost.

Example reverse-proxy target:

```text
https://portal.hugo.dk/memory -> http://127.0.0.1:8888
```

Hermes should point at the public HTTPS URL. Local tools can still point at
`http://127.0.0.1:8888` when running on the same host.

## Priority 2: OpenClaw

OpenClaw uses the same standalone server through `extensions/openclaw`.

```bash
cd extensions/openclaw
npm install
npm run build
```

Configure OpenClaw with:

```text
serverUrl = https://portal.hugo.dk/memory
workspace = personal-openclaw
apiKey = optional-bearer-token
autoRecall = true
autoCapture = true
```

OpenClaw should remain a thin adapter. It must not duplicate storage,
retrieval, reflection, or ranking logic from the server.

## Troubleshooting

- `singularity_memory_status` fails: verify `server_url`, bearer token, and
  that `/v1/banks` is reachable from the Hermes host.
- Recall returns empty results: store a known fact with
  `singularity_memory_store`, then search for an exact phrase.
- Core memory is missing: verify the server supports `/core-memory` endpoints
  and that the configured workspace is correct.
- Prompt output contains memory tags from stored content: this is a bug; the
  adapter must sanitize nested memory fences before injection.
- OpenClaw uses the wrong memories: check the configured `workspace`; each
  agent/client should use an intentional workspace name.

## Workspace Isolation

Each agent or user should use a distinct workspace name. The server enforces
bank isolation at the workspace level: memories stored in `workspace-A` do not
appear in recall results for `workspace-B`. This means:

- Hermes and OpenClaw can share the same server but maintain separate memory
  banks by using different `workspace` values in their configs.
- Rotating a workspace name effectively creates a fresh memory bank without
  deleting existing data — useful for testing or resetting an agent.
- The `/v1/banks` endpoint lists all banks across all workspaces; individual
  clients only see their own workspace's bank via scoped endpoints.

## MCP Endpoint

The MCP (Model Context Protocol) endpoint at `/mcp/` allows MCP-compatible
clients such as Claude Code, Cursor, and Windsurf to use the memory server
without a dedicated adapter plugin. Configure the MCP client with:

```
URL: https://portal.hugo.dk/memory/mcp/
Authorization: Bearer <your-api-key>
```

The MCP wire protocol is JSON-RPC over HTTP POST, matching the Hermes tool
schema names so that MCP recipes and the Hermes adapter are interchangeable.

## Contract

Standalone mode is first-class. ACE may embed or back this service later, but
the Hermes and OpenClaw provider contract must remain stable.
