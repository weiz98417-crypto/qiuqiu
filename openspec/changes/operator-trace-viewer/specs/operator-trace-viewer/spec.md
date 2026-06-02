# Operator Trace Viewer Specification

## ADDED Requirements

### Requirement: Operator trace listing

The backend MUST provide an operator-authenticated endpoint for listing recent QiuQiu decision traces for a match.

#### Scenario: Authorized trace list

- **GIVEN** a match has one or more agent trace records
- **WHEN** the operator requests `/api/matches/{matchId}/traces` with a valid token
- **THEN** the backend returns a JSON list of traces
- **AND** each trace includes id, match id, input, intent, output, reason, and created time

#### Scenario: Unauthorized trace list

- **GIVEN** operator auth is enabled
- **WHEN** the request does not include a valid token
- **THEN** the backend returns unauthorized
- **AND** no trace payload is returned

### Requirement: Operator trace detail

The backend MUST provide an operator-authenticated endpoint for inspecting one trace in detail.

#### Scenario: Detail includes tool calls

- **GIVEN** a trace includes memory tool calls
- **WHEN** the operator requests that trace detail
- **THEN** the response includes structured tool call names and arguments
- **AND** the response includes retrieved event IDs

#### Scenario: Missing trace

- **GIVEN** the requested trace id does not exist for the match
- **WHEN** the operator requests that trace detail
- **THEN** the backend returns not found

### Requirement: Trace viewer UI

The operator console MUST expose a read-only trace view for the active match.

#### Scenario: Trace table

- **GIVEN** traces exist for the active match
- **WHEN** the operator opens the trace view
- **THEN** the UI shows a table or list of recent traces
- **AND** each row shows time, input preview, intent, output preview, and reason

#### Scenario: Trace detail drawer

- **GIVEN** the operator selects a trace row
- **WHEN** the detail view opens
- **THEN** the UI shows full input, full output, intent, tool calls, retrieved event IDs, reason, and error state

### Requirement: Trace safety

Trace records MUST NOT expose secrets or mutable match controls.

#### Scenario: No secrets in trace payload

- **GIVEN** trace data is returned to the operator UI
- **WHEN** the trace is serialized
- **THEN** API keys, tokens, provider secrets, and environment values are absent

#### Scenario: Read-only trace interaction

- **GIVEN** the operator is viewing traces
- **WHEN** the operator interacts with trace rows
- **THEN** the UI does not offer edit, delete, or correction controls for trace records
