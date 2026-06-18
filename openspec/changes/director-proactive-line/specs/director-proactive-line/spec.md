# Director Proactive Line Specification

## ADDED Requirements

### Requirement: Director event contract for proactive output

Director-published match events MUST preserve enough structured data for QiuQiu to speak proactively and answer follow-up questions.

#### Scenario: Goal event payload

- **GIVEN** the director publishes a goal from the live console
- **WHEN** the event reaches the backend
- **THEN** the stored event includes match ID, period, clock, event type, team, score, intensity, description, source, and participants
- **AND** participants can include `scorer`, `assist`, `pre_assist`, `defender`, and `keeper`
- **AND** the event source is recorded as operator-authored

#### Scenario: Incomplete event payload

- **GIVEN** a director event is missing required match time or event type
- **WHEN** the backend validates the event
- **THEN** the backend rejects or normalizes the event deterministically
- **AND** the trace or response includes a reason that can be inspected later

### Requirement: Proactive publication modes

The director console MUST support proactive, quiet, manual, and deterministic fallback publication modes.

#### Scenario: Proactive goal reaches the user app

- **GIVEN** the director publishes Pedri's goal in proactive mode
- **WHEN** the event is committed as an active fact
- **THEN** the user app receives a QiuQiu proactive message
- **AND** the message references the published event
- **AND** optional MiMo TTS may be requested for the proactive message

#### Scenario: Manual director line is preserved

- **GIVEN** the director writes a proactive line manually
- **WHEN** the event is published in manual mode
- **THEN** QiuQiu emits the director-written line exactly as the user-facing proactive text
- **AND** the trace records that the line came from the director

#### Scenario: Quiet event is recorded without proactive speech

- **GIVEN** the director publishes a tactical or low-priority event in quiet mode
- **WHEN** the event is committed
- **THEN** the event is available for future QiuQiu memory
- **AND** no proactive user message is emitted

### Requirement: Follow-up questions link to proactive events

User follow-up questions after a proactive event MUST resolve against the same active match event unless the director has corrected it.

#### Scenario: Assist follow-up after a goal

- **GIVEN** QiuQiu proactively reacted to a goal with `assist=法比安`
- **WHEN** the user asks "谁助攻？"
- **THEN** QiuQiu answers that Fabian assisted
- **AND** the trace references the goal event ID

#### Scenario: Build-up follow-up after assist question

- **GIVEN** the prior referenced event has `pre_assist=亚马尔`
- **WHEN** the user asks "谁策动的？"
- **THEN** QiuQiu answers that Yamal participated in the build-up
- **AND** QiuQiu does not invent participants absent from the event

### Requirement: Correction-aware proactive facts

Director corrections MUST keep history auditable while ensuring QiuQiu answers from active replacement facts only.

#### Scenario: Corrected goal event

- **GIVEN** the director corrects a previously published goal event
- **WHEN** a replacement event is committed
- **THEN** the original event is marked corrected
- **AND** the replacement event becomes active
- **AND** future QiuQiu answers use the replacement facts
- **AND** traces can still show both event IDs for audit
