# Delivery Demo Package Specification

## ADDED Requirements

### Requirement: Repeatable demo runbook

The project MUST include a customer-demo runbook that takes a presenter through the full football digital human flow.

#### Scenario: Ten-minute demo path

- **GIVEN** the presenter has the repository and required MiMo environment variables when voice is needed
- **WHEN** they follow the demo runbook
- **THEN** they can start the backend, seed Spain vs Germany, open the user app, open the director console, publish or verify Pedri's goal, ask a follow-up, and inspect traces
- **AND** the flow can be completed in under 10 minutes after dependencies are installed

#### Scenario: No external sports-data key required

- **GIVEN** the presenter follows the demo runbook
- **WHEN** they configure required environment variables
- **THEN** the runbook asks for MiMo voice/model configuration when voice is needed
- **AND** it does not ask for an external sports-data key

### Requirement: Demo smoke scripts

The project MUST provide documented smoke checks that verify the demo path before presentation.

#### Scenario: Text-only pre-demo verification

- **GIVEN** the backend is running without `MIMO_API_KEY`
- **WHEN** the presenter runs the documented text-only smoke sequence
- **THEN** health, seed, match state, user follow-up, and trace output are verified
- **AND** the demo remains usable through text fallback

#### Scenario: Voice-enabled pre-demo verification

- **GIVEN** `MIMO_API_KEY` is available in the process environment
- **WHEN** the presenter runs the documented voice smoke sequence
- **THEN** MiMo ASR, MiMo TTS, and browser voice UI affordances are verified
- **AND** no real secret is written to docs, tests, OpenSpec, or source files

### Requirement: Customer-facing explanation package

The project MUST include customer-facing material that explains the business value and architecture of QiuQiu.

#### Scenario: Architecture explanation

- **GIVEN** a customer asks how the system stays grounded during a live match
- **WHEN** the presenter opens the introduction or runbook material
- **THEN** the material explains director-authored facts, agent memory, voice output, and traceability
- **AND** it makes clear that the director console is the authority for realtime match facts

#### Scenario: Enterprise value explanation

- **GIVEN** the presenter uses the material with an enterprise customer
- **WHEN** they describe the project
- **THEN** the material explains live-event digital human value, auditability, controllability, and reusable architecture patterns
- **AND** it avoids self-deprecating learner-oriented framing

### Requirement: Acceptance checklist

The delivery package MUST include checklists for text-only demo, voice demo, trace explanation, and correction authority.

#### Scenario: Presenter checks demo readiness

- **GIVEN** a presenter is preparing for a customer demo
- **WHEN** they complete the acceptance checklist
- **THEN** they know whether text chat, director proactive output, voice, memory follow-up, trace view, and correction story are ready
- **AND** troubleshooting steps exist for port conflicts, microphone denial, ASR failure, TTS failure, and stale demo state
