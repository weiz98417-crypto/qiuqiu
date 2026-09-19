# Delivery Demo Package

## Why

The project needs to be understandable and repeatable for customers, not just functional for developers. A delivery package should let a presenter start the app, run the canonical match, demonstrate voice and memory, and explain the enterprise value without improvising.

## What Changes

- Create a complete demo script for the football digital human.
- Ensure one-command or clearly documented setup for seed, smoke, user app, director console, and traces.
- Package customer-facing explanations around director facts, QiuQiu memory, voice interaction, and traceability.
- Add final smoke checks for the complete demo path.
- Update project introduction material to reflect the current MiMo and director-console architecture.

## Non-goals

- Do not add external sports-data API dependencies.
- Do not include real API keys in files or screenshots.
- Do not optimize for production deployment in this change.
- Do not create a marketing landing page instead of a usable demo package.

## Success Criteria

- A presenter can complete the full demo in under 10 minutes.
- The demo does not require a sports data key.
- With `MIMO_API_KEY`, voice interaction works; without it, text fallback still works.
- The customer-facing material explains the business value clearly.
- All smoke scripts pass before handoff.
