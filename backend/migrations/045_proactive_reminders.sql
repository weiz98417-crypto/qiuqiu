-- Proactive reminders (openspec/changes/proactive-scheduler): user-requested
-- pre-match nudges, delivered server-side following the
-- observation_resolution_outbox pattern (migration 020): persist pending →
-- deliver as qiuqiu_reply on next connect / online beat → flip status.
-- Missed reminders expire at kickoff+30min and turn into memory material.
CREATE TABLE IF NOT EXISTS proactive_reminders (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL DEFAULT '',
  home_team TEXT NOT NULL DEFAULT '',
  away_team TEXT NOT NULL DEFAULT '',
  kickoff_at TIMESTAMPTZ NOT NULL,
  lead_minutes INT NOT NULL DEFAULT 30,
  timezone TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'suppressed')),
  deliver_at TIMESTAMPTZ NOT NULL,
  expire_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_proactive_reminders_user_status
  ON proactive_reminders(user_id, status);

CREATE INDEX IF NOT EXISTS idx_proactive_reminders_status_deliver
  ON proactive_reminders(status, deliver_at);
