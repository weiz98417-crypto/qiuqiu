# Agent Depth: From Reflexes to Inner Life

## Why

Benchmark research (Stanford Generative Agents, Neuro-sama, XiaoIce, Replika, Open-LLM-VTuber) shows the "aliveness" gap is not engineering quality — QiuQiu's fact ledger, policy-table persona, decision/expression split, and turn scheduler are already strong. The gap is a missing inner-life layer:

- **Memory is a rolling window.** `conversation.read_recent` surfaces only recent turns; nothing scores importance, retrieves by relevance, or synthesizes insights. QiuQiu cannot reference last week's conversation, so continuity ("old ballmate") is impossible.
- **Proactivity is a reflex.** `proactive_gate.go:15-17` fires on operator mode + event whitelist + 90s cooldown. CONTEXT.md defines a Proactive Turn as *justified by match context, an open thread, or a shared moment* — the justification machinery does not exist.
- **The body underacts.** The model ships 17 motions in 12 semantic groups plus 7 expressions, but the client whitelist exposes only 7 motions; mouth movement during speech is random jaw jitter (`live2d_view.dart:375-385`); the affect vector is computed in five dimensions but presentation consumes one.
- **Known drift to fix:** the client sends the user's talkativeness setting (3 tiers) with every turn; the backend never parses it (`main.go:861-875` reads only text/audio/userId/signalId/timezone).

## What Changes

- **C4 · Body**: expose the full motion inventory through the presentation whitelist (7 → 12 groups / 17 motions; ADR-0005 contract structure unchanged); replace random jaw jitter with real lip sync (wLipSync WASM + pixi-live2d-display lipsync patch driving mouth parameters from TTS audio); map the affect vector to 3 idle tiers (deflated / calm / energetic) so the body acts during quiet stretches.
- **C1 · Memory**: define an in-domain memory seam (`Memories`: Observe / Recall / Portrait / Threads, ADR-0006) with **Memobase** as the first adapter (official Go SDK, dockerized, profile-based). Local Interaction Ledger remains the immutable, auditable fact stream (division of labor per the "Redis is not the source of truth" philosophy). Observation writes are asynchronous and never block a watch turn; reflection runs as a scheduled beat and synthesizes user insights back into recall.
- **C3 · User model**: synthesize a user Portrait (facts, preferences, emotional patterns) from policy flags + reflection output, inject it into realization context, and expose it on a client page that is visible, editable, and deletable via the existing privacy lifecycle. The portrait must be actually wired into prompts — Replika's unwired "diary" is the counterexample.
- **C2 · Inner life**: an Open Thread ledger (unanswered questions, promises, emotional moments — concept already named in CONTEXT.md, unimplemented) consumed by planner beats (in-match event-driven, post-match recovery, idle reflection). Every proactive turn must cite a thread or shared moment as its reason code. Wire the talkativeness setting into initiative frequency, fixing the drift.

## Non-goals

- No push notifications or out-of-session touch in v1 (L3-class; separate design).
- No Live2D model/asset replacement; no new expression engine (ADR-0005: PresentationPlan stays the single contract — C4 only widens the whitelist).
- The Interaction Ledger stays append-only and immutable; nothing edits or moves it into Memobase.
- No vector search replacing structured match queries (carried over from companion-agent-upgrade).
- Memory synthesis never creates, corrects, or asserts Match Facts (ForbiddenClaims discipline unchanged).
