---
status: accepted
---

# All Watch Turns use unified TurnPlan planning

Every user signal, match change, first meeting, and delivery result enters one Watch Turn planning flow and produces a validated TurnPlan before realization or delivery. Grounding decides the factual mode, CompanionDirector decides relationship and communication policy, and LLM/media Adapters only realize or deliver within that plan; clients and transport handlers cannot alter domain decisions. This keeps Character Stance, User Boundary, Chosen Silence, and factual safety in one testable seam instead of creating parallel digital-human behaviors.

## Considered Options

- Keep separate user, proactive-event, and legacy AIPipeline paths: rejected because the same event could receive different relationship, fact, or presentation rules.
- Let the LLM choose intent, initiative, and factual wording first: rejected because safety and reproducibility would depend on an opaque provider response.

## Consequences

- A TurnPlan may select up to two Communication Acts, with at most one question; Chosen Silence is an explicit outcome.
- Critical fact revisions may trigger one refresh using the same Signal ID; stale results are never delivered.
- The old parallel AIPipeline/WatchSession generation path is removed after contract and shadow tests pass.
