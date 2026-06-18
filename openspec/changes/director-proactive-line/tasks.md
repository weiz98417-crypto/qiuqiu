## 1. Event Contract

- [x] 1.1 Verify director event payload includes match time, score, event type, team, participants, description, and proactive text.
- [x] 1.2 Verify participants support scorer, assist, pre_assist, defender, keeper, and other role slots used by the logic combiner.
- [x] 1.3 Verify `source=operator` and optional `operatorId` are preserved.
- [x] 1.4 Add evals for malformed or incomplete director events.

## 2. Proactive Output

- [x] 2.1 Verify proactive mode emits QiuQiu text to the user app.
- [x] 2.2 Verify manual mode preserves director-written proactive text exactly.
- [x] 2.3 Verify quiet mode records the event without proactive output.
- [x] 2.4 Verify fallback proactive text is deterministic and grounded in the event.
- [x] 2.5 Verify high-intensity events trigger event-specific Live2D actions.

## 3. Follow-up Linking

- [x] 3.1 Verify "谁助攻？" resolves to the most recent active goal event.
- [x] 3.2 Verify "谁策动的？" follows the prior referenced event.
- [x] 3.3 Verify follow-up questions do not invent participants not in the event.
- [x] 3.4 Verify user conversation turns retain the referenced event ID.

## 4. Correction Path

- [x] 4.1 Verify director correction marks the original event as corrected.
- [x] 4.2 Verify replacement event becomes active and visible in snapshot.
- [x] 4.3 Verify QiuQiu answers from corrected active facts only.
- [x] 4.4 Verify corrected operator facts remain auditable in trace/history.

## 5. E2E And QA

- [x] 5.1 Add an end-to-end eval for director publish -> proactive QiuQiu output.
- [x] 5.2 Add an end-to-end eval for proactive output -> user follow-up -> grounded answer.
- [x] 5.3 Add an end-to-end eval for correction -> follow-up answer uses replacement fact.
- [x] 5.4 Run demo smoke after proactive-line changes.
