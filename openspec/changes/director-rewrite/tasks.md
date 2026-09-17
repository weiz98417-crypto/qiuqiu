# Tasks: Director Rewrite

## 1. Event model port

- [ ] 1.1 Port the 18-event model (roles/actions/intensity) to `console/src/director/event-model.ts`; snapshot test against `operator-live-state.js` (byte-equal payload shapes).

## 2. Draft cards

- [ ] 2.1 Event draft card: antd Form per event (fact status, score correction + reason, 球球处理 auto/quiet/manual + manual textarea, templates, preview) — exact request shapes per operator-control evals.
- [ ] 2.2 Behavior bar: draft-only buttons (绝不直接写), busy states on all submissions.

## 3. Voice draft flow

- [ ] 3.1 getUserMedia capture → POST drafts/voice → conflict cards → drafts/voice/publish; port the legacy page's capture approach.

## 4. Fact timeline

- [ ] 4.1 antd Timeline with proactive line display ("球球主动说："), fact status badges, revisions link.

## 5. Retirement

- [ ] 5.1 Parity checklist: every legacy #live feature mapped to a new component (or explicitly dropped with director sign-off).
- [ ] 5.2 One live-match dress rehearsal on the new page.
- [ ] 5.3 Flip the console nav default to the new director page; keep `operator.html#live` one release; then delete `operator.html` (final).
