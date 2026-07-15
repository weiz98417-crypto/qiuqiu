DROP INDEX IF EXISTS idx_interaction_decisions_user_signal;

CREATE UNIQUE INDEX IF NOT EXISTS idx_interaction_decisions_user_match_signal
  ON interaction_decisions(user_id, match_id, signal_id);
