# Standalone Provider Contract

Singularity Memory is a freestanding memory provider for agents. ACE may use,
vendor, wrap, or back it, but ACE must not become the only way to run it.

## First-Class Clients

- Priority 1: Hermes personal agents, including hosted deployments such as
  `portal.hugo.dk`, through `extensions/hermes`.
- Priority 2: OpenClaw agents through `extensions/openclaw`.
- MCP-aware tools through the `/mcp/` endpoint and `extensions/mcp`.
- SF, Singularity Runner, ACE, or other repo-local agents through HTTP.

## Required Standalone Modes

- Local developer server: `docker compose up singularity-memory-postgres
  singularity-memory`.
- Hosted/tailnet server: a long-running HTTP+MCP service behind normal auth,
  reachable by Hermes/OpenClaw clients.
- Optional/free provider: clients can choose Singularity Memory as their memory
  provider without adopting ACE.
- ACE-backed provider: ACE may implement the backend later, but must preserve
  the same client-visible retain/recall/reflect/core-memory semantics.

## Adapter Rules

- Hermes is the primary provider path. Keep its memory-provider adapter
  working before expanding the OpenClaw path.
- Hermes and OpenClaw adapters stay thin. They should not duplicate retrieval,
  ranking, reflection, or storage logic.
- Adapters must fence and sanitize recalled memory before injecting it into a
  prompt.
- The server owns durable state: banks, workspaces, core memory, mental models,
  directives, feedback, reflection, summarize/offload, operation logs, and
  evidence export.
- Agent runtimes own action: tool calls, file edits, shell execution, UI, and
  approvals.

## ACE Relationship

ACE can learn from and integrate this service, but the standalone service
remains supported. Changes that alter the public HTTP/MCP/provider behavior
must keep Hermes and OpenClaw compatibility intact.
