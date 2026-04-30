# Go Migration Parity Contract

This contract defines when Go can replace a Python-served HTTP endpoint or MCP
tool. The answer must be provable from local repository artifacts, without a
live Python runtime in the parity gate.

## Sources Of Truth

- Frozen HTTP schema: `openapi.json`.
- OpenAPI freeze history: `scripts/dump-openapi.py` records how the current
  artifact was produced. It is not a Go migration gate.
- Current Go HTTP implementation: `go/internal/httpapi/server.go` and
  `go/internal/httpapi/banks.go`.
- Current Go HTTP behavior tests: `go/internal/httpapi/server_test.go`.
- Current bank success fixtures:
  `go/internal/httpapi/testdata/fixtures/bank_*.json`.
- MCP transport and tool history:
  `src/singularity_memory_server/api/mcp.py`,
  `src/singularity_memory_server/mcp_tools.py`, and `extensions/mcp/README.md`.

## Current Go Parity Status

Implemented and locally tested in Go:

- `GET /healthz`
- `GET /version`
- `GET /openapi.json`
- `GET /v1/banks`
- `GET /v1/default/banks`
- `GET /v1/default/banks/{bank_id}/profile`
- `PUT /v1/default/banks/{bank_id}/profile`
- `PUT /v1/default/banks/{bank_id}`
- `PATCH /v1/default/banks/{bank_id}`
- `DELETE /v1/default/banks/{bank_id}`

Committed success fixtures currently cover bank list, bank profile, bank
update, and bank delete responses. Error and storage-edge fixtures are still
required before the bank surface is accepted for traffic flip.

Everything else in `openapi.json` remains frozen contract surface but is not
yet proven as Go parity.

## No Python Runtime In Parity Gates

Do not make Go parity depend on importing FastAPI, starting the Python server,
or regenerating schemas during the gate. Endpoint migration must pass using
committed artifacts only:

- `openapi.json`;
- HTTP request/response fixtures;
- MCP tool-list and tool-call transcripts;
- deterministic database/store fixtures;
- Go test code.

## Fixture Comparison Rules

All parity fixtures should be normalized before comparison:

- status code must match exactly;
- JSON response bodies are parsed, canonicalized with sorted object keys, and
  re-encoded before comparison;
- arrays remain ordered unless the fixture explicitly marks an array as
  unordered;
- timestamps normalize to RFC3339 UTC;
- volatile IDs, request IDs, operation IDs, elapsed times, and generated
  messages may be normalized only when the fixture declares the field volatile;
- omitted, `null`, and empty values are distinct unless the frozen contract
  explicitly allows them to collapse;
- validation errors and error envelopes are part of the contract.

Any normalization that changes observable JSON must be documented beside the
fixture and justified as client-invisible.

## Endpoint Acceptance Gate

A Go HTTP route is accepted only when:

1. Its path and methods exist in `openapi.json`, or the contract doc explains
   why it is Go-only operational surface such as `/healthz`.
2. The Go route is registered intentionally and, for migrated API surface,
   feature-flagged until parity is proven.
3. Fixtures cover at least one success response, one validation/error response,
   and one storage edge case where the endpoint touches storage.
4. A Go test drives the route through `net/http` and compares normalized output
   to committed fixtures.
5. CI can run the test without Python.

## MCP Acceptance Gate

A Go MCP transport/tool is accepted only when:

1. `/mcp/` and `/mcp/{bank_id}/` preserve the documented bank resolution,
   auth, GET-probe, and Accept-header behavior.
2. `tools/list` output matches the frozen tool list for multi-bank and
   single-bank modes after allowed ordering normalization.
3. Each ported tool has committed tool-call transcripts for success and error
   behavior.
4. Unknown argument stripping and string-encoded JSON coercion remain compatible
   where current MCP clients rely on them.
5. CI replays transcripts against Go without Python.

## Change Discipline

- Do not edit `openapi.json` by hand after freeze.
- Do not widen a Go response shape because it is easier to implement.
- Add new HTTP endpoints to OpenAPI first, then add fixtures, then implement Go.
- Add new MCP tools by freezing `tools/list` schema and at least one call
  transcript before enabling them by default.
- Keep unsupported Go routes dark behind feature flags until their parity gate
  passes.
