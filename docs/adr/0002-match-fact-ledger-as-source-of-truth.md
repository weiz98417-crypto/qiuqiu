---
status: accepted
---

# Match Fact Ledger is the sole source of truth

Qiuqiu treats the append-only Match Fact Ledger and deterministic replay projection as the sole authority for match facts. External sources may submit provisional facts, while operator actions confirm, reconcile, or revoke them; memory and PostgreSQL remain interchangeable Adapters around the same domain rules. This preserves correction history, makes snapshots reproducible, and prevents the conversation layer from inventing or silently overwriting match state.

## Considered Options

- Keep independent validation and projection logic in memory and PostgreSQL: rejected because behavior would drift and replay would not be authoritative.
- Let the conversation or operator layer patch snapshots directly: rejected because delivered replies could no longer be traced to a revisioned fact.

## Consequences

- Every correction, conflict, and public-visibility change is represented as a new ledger fact or revision.
- Projector versions must be recorded and replayable; materialized snapshots are rebuildable views.
- User Fact Claims never write the Match Fact Ledger directly.
