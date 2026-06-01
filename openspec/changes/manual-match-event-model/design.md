# Design

## Event Contract

Each event should be stored as a structured object:

```json
{
  "id": "evt_...",
  "matchId": "match_...",
  "source": "operator",
  "period": "first_half",
  "clock": "23:15",
  "eventType": "goal",
  "teamId": "argentina",
  "teamName": "Argentina",
  "playerName": "Lionel Messi",
  "score": { "home": 1, "away": 0 },
  "intensity": 5,
  "sentiment": "celebratory",
  "description": "Back-post header after a right-side cross.",
  "tags": ["set_piece", "header"],
  "recommendedAction": "celebrate",
  "visibility": "public",
  "createdAt": "2026-05-22T00:00:00.000Z",
  "updatedAt": "2026-05-22T00:00:00.000Z",
  "revisionOf": null,
  "status": "active"
}
```

## Required Fields

- `matchId`
- `source`
- `period`
- `clock`
- `eventType`
- `description`
- `intensity`
- `status`

## Event Types

Initial event type set:

- `kickoff`
- `goal`
- `shot`
- `big_chance`
- `save`
- `miss`
- `foul`
- `yellow_card`
- `red_card`
- `var_check`
- `penalty`
- `substitution`
- `injury`
- `tactical_shift`
- `pressure`
- `halftime`
- `fulltime`
- `operator_note`

## Source Strategy

`source` starts with `operator`. Future values may include `provider`, `user`, and `system`.

Manual and provider events must share the same normalized contract so the frontend does not care where the event came from.

## Revision Strategy

Operators need to correct mistakes. Do not delete events by default. Mark the original as `corrected` and create a new event with `revisionOf` pointing to the original event id.

## Action Mapping

The model does not own final Live2D behavior, but it may carry `recommendedAction`. The companion layer can override this based on current state, cooldown, or user mood.
