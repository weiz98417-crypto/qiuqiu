-- Subscriptions (openspec/changes/season-subscription): team-level reminder
-- subscriptions expanded into proactive_reminders by the daily beat.
CREATE TABLE IF NOT EXISTS proactive_subscriptions (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  team_name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'cancelled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_proactive_subs_user_team_active
  ON proactive_subscriptions(user_id, team_name) WHERE status = 'active';

-- Link reminders expanded from a subscription (citation subscription:<id>).
ALTER TABLE proactive_reminders ADD COLUMN IF NOT EXISTS subscription_id TEXT NOT NULL DEFAULT '';
