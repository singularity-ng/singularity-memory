# Architecture

This file is the short map of the codebase. Keep it current and compact.

## Purpose

Describe the product, its users, and the job this repository exists to do.

## Codemap

- `src/`: primary implementation.
- `tests/`: behavior and regression coverage.
- `docs/`: durable product, design, plan, reliability, and security context.

## Invariants

- Prefer small, named modules with clear ownership.
- Behavior changes need tests or an explicit eval.
- Keep generated artifacts out of hand-written design docs.
- Update this map when new top-level concepts or directories become important.
