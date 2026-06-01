# Companion Explorer Specification

## ADDED Requirements

### Requirement: Memory-grounded user replies

QiuQiu MUST answer user-initiated match questions using recorded match memory before using any language-model freeform response.

#### Scenario: Current score question

- **GIVEN** the director has recorded a match snapshot for Spain vs Germany with score `1-0`
- **WHEN** the user asks "现在几比几？"
- **THEN** QiuQiu answers with Spain `1-0` Germany
- **AND** the answer includes the current period or clock when available

#### Scenario: Assist question after a goal

- **GIVEN** the director has recorded a goal with `scorer=佩德里`, `assist=法比安`, and `pre_assist=亚马尔`
- **WHEN** the user asks "刚才谁助攻？"
- **THEN** QiuQiu answers that Fabian assisted
- **AND** QiuQiu may mention that Yamal participated in the build-up

#### Scenario: Missing player fact

- **GIVEN** no active event records Musiala scoring
- **WHEN** the user asks "穆西亚拉刚才进球了吗？"
- **THEN** QiuQiu says that no Musiala goal is currently recorded
- **AND** QiuQiu MUST NOT guess or imply an unrecorded goal

### Requirement: Deterministic intent routing

The explorer MUST route user input into stable intent categories before building a reply.

#### Scenario: Match status intent

- **WHEN** the user asks for score, match time, or current state
- **THEN** the intent is `match_status_question`
- **AND** the explorer calls the snapshot memory tool

#### Scenario: Recent event intent

- **WHEN** the user asks about "刚才", "上一个", "谁助攻", or a recent event
- **THEN** the intent is `recent_event_question`
- **AND** the explorer calls a recent-event memory tool

#### Scenario: Control command intent

- **WHEN** the user says "少说一点", "安静一点", or "别说话了"
- **THEN** the intent is `control_command`
- **AND** the explorer updates or returns a talkativeness policy response instead of querying match facts

### Requirement: Traceable companion decisions

The explorer MUST persist enough trace data to reconstruct why QiuQiu gave an answer.

#### Scenario: Trace for a factual answer

- **GIVEN** the user asks "刚才谁助攻？"
- **WHEN** QiuQiu answers using a recorded goal event
- **THEN** the trace records the input text, intent, tool calls, retrieved event IDs, output text, and reason

#### Scenario: Trace for missing facts

- **GIVEN** no active event supports the user's requested fact
- **WHEN** QiuQiu answers with a safe "not recorded" response
- **THEN** the trace records the attempted lookup
- **AND** the trace records that the response was produced by missing-fact policy

### Requirement: Correction-aware memory

The explorer MUST ignore corrected match events when answering active match questions.

#### Scenario: Corrected goal

- **GIVEN** a goal event has been corrected by a later VAR or correction event
- **WHEN** the user asks about the current score or whether the goal counts
- **THEN** QiuQiu answers from the active snapshot
- **AND** QiuQiu does not treat the corrected goal as active truth
