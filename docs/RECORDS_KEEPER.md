# Records Keeper

The records keeper keeps repo memory ordered after meaningful changes. Run this checklist at milestone close, after architecture changes, after product behavior changes, and whenever docs/source disagree.

Use the `records-keeper` skill for this workflow when SF skills are available. Use `context-doctor` instead when stale state lives under `.sf/` or the memory store.

## Canonical Homes

- Root `AGENTS.md`: short routing map for agents.
- `ARCHITECTURE.md`: short system map, boundaries, invariants, critical flows, and verification.
- `docs/product-specs/`: durable user-facing behavior and product decisions.
- `docs/design-docs/`: durable design and architecture decisions.
- `docs/exec-plans/`: active/completed work plans and technical debt.
- `docs/generated/`: generated references only.
- `docs/records/`: audits, ledgers, and context-gardening outputs.

## Checklist

- Root map is current: `AGENTS.md` points to the right canonical docs and local `AGENTS.md` files.
- Architecture is current: new subsystems, boundaries, invariants, data/state, or critical flows are reflected in `ARCHITECTURE.md`.
- Product specs are current: user-visible behavior changes are reflected in `docs/product-specs/`.
- Execution plans are filed: active work is in `docs/exec-plans/active/`; completed summaries and evidence are in `docs/exec-plans/completed/`.
- Debt is visible: discovered cleanup is listed in `docs/exec-plans/tech-debt-tracker.md`.
- Generated docs are marked: generated material stays under `docs/generated/` or clearly says how to regenerate it.
- Contradictions are resolved: stale docs are updated or marked superseded with links to the source of truth.
- Verification is recorded: changed checks, evals, and commands are listed in the relevant plan or quality document.

## Output

When records work is non-trivial, write a dated note under `docs/records/` with:

- What changed.
- What canonical docs were updated.
- What contradictions were found.
- What remains unresolved.
