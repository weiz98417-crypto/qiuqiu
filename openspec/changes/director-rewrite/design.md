# Design: Director Rewrite

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Four-step rewrite (event model → draft forms → voice flow → fact timeline), each independently mergeable, legacy page stays live throughout. |
| 2 | Retirement standard: 28 operator-control evals green on the new page + parity checklist + director sign-off; only then delete `operator.html#live`. |
| 3 | Everything keeps the idempotent-write infrastructure and exact request shapes (the 28 evals assert them). |

## Port map (from operator.html / operator-live-state.js)

| Legacy | New home |
| --- | --- |
| 18-event model + roles/actions/intensity | `console/src/director/event-model.ts` (+ snapshot test vs legacy file) |
| Event draft form (fact status, score correction + reason, manual proactive auto/quiet/manual, templates + preview) | `console/src/director/DraftCard.tsx` (antd Form) |
| Fact timeline + proactive line display | `console/src/director/FactTimeline.tsx` (antd Timeline) |
| Voice capture → drafts/voice → conflict cards → publish | `console/src/director/VoiceDraft.tsx` (getUserMedia, same backend endpoints) |
| Behavior buttons (draft-only semantics) | `console/src/director/BehaviorBar.tsx` |

## Steps

1. Event model module + snapshot test (port, no behavior change).
2. Draft cards (forms + writes via the same idempotent endpoints).
3. Voice draft flow.
4. Fact timeline.
5. Parity checklist + flip the default + retirement of `operator.html#live`.
