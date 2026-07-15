ALTER TABLE interaction_decisions
  DROP CONSTRAINT IF EXISTS interaction_decisions_signal_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_interaction_decisions_user_match_signal
  ON interaction_decisions(user_id, match_id, signal_id);
