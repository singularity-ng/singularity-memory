# MCP JSON-RPC 2.0 HTTP Wire Protocol

This package implements the Model Context Protocol (MCP) JSON-RPC 2.0 HTTP transport for the Singularity Memory server.

## Overview

- `types.go` — JSON-RPC 2.0 request/response/error types and content blocks.
- `session.go` — In-memory session store (`sync.Map`) keyed by `Mcp-Session-Id`.
- `server.go` — MCP `Server` HTTP handler with auth, session management, and dispatch.
- `handlers.go` — `initialize`, `tools/list`, and `tools/call` handlers.
- `tools.go` — `ToolBackend` interface for the real tool execution layer.

## Supported Methods

| Method | Description |
|--------|-------------|
| `initialize` | Initialize a session and return server capabilities. |
| `tools/list` | List available tools (`memory_retain`, `memory_recall`, `memory_list_banks`, `memory_get_bank`). |
| `tools/call` | Invoke a tool with arguments. |

## Authentication

Set `SINGULARITY_MCP_AUTH_TOKEN` to require `Authorization: Bearer <token>` on all requests.

## Bank Routing

The `X-Bank-Id` header sets the default bank for the session. It can be overridden per-request.

## Usage

```go
srv := mcp.NewServer()
srv.ToolBackend = myBackend
http.Handle("/mcp", srv)
```

## Testing

```bash
go test ./internal/mcp/... -v
```
