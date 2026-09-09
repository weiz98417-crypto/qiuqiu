CREATE INDEX IF NOT EXISTS idx_interaction_ledger_user_match_cursor
  ON interaction_ledger (
    user_id,
    match_id,
    created_at,
    (CASE
      WHEN kind <> 'playback_result' THEN 0
      WHEN playback_state = 'started' THEN 1
      WHEN playback_state IN ('ended', 'completed', 'interrupted', 'skipped', 'blocked') THEN 2
      ELSE 3
    END),
    id
  );
