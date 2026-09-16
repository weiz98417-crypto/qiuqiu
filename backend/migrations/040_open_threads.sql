CREATE TABLE IF NOT EXISTS open_threads (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('unanswered_question', 'promise', 'emotional_moment', 'prediction')),
  content TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'addressed', 'expired')),
  source_turn TEXT NOT NULL DEFAULT '',
  ledger_sequence BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_open_threads_user_state
  ON open_threads(user_id, state);

-- User-level preference tier carried by every user_speech payload
-- (quiet/normal/active); drives the proactive frequency caps in C2.
CREATE TABLE IF NOT EXISTS user_preferences (
  user_id TEXT PRIMARY KEY,
  talkativeness TEXT NOT NULL DEFAULT 'normal' CHECK (talkativeness IN ('quiet', 'normal', 'active')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
