CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS matches (
  id TEXT PRIMARY KEY,
  home_team TEXT NOT NULL,
  away_team TEXT NOT NULL,
  competition TEXT NOT NULL DEFAULT '',
  kickoff TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS match_players (
  id BIGSERIAL PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  team_id TEXT NOT NULL CHECK (team_id IN ('home', 'away')),
  team_name TEXT NOT NULL DEFAULT '',
  number TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  position TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (match_id, team_id, name)
);

CREATE TABLE IF NOT EXISTS match_events (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  source TEXT NOT NULL DEFAULT 'operator',
  provider_name TEXT NOT NULL DEFAULT '',
  operator_id TEXT NOT NULL DEFAULT '',
  period TEXT NOT NULL,
  clock TEXT NOT NULL,
  event_type TEXT NOT NULL,
  team_id TEXT NOT NULL DEFAULT '',
  team_name TEXT NOT NULL DEFAULT '',
  player_name TEXT NOT NULL DEFAULT '',
  score_home INT NOT NULL DEFAULT 0,
  score_away INT NOT NULL DEFAULT 0,
  intensity INT NOT NULL DEFAULT 3 CHECK (intensity BETWEEN 1 AND 5),
  sentiment TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL,
  proactive_text TEXT NOT NULL DEFAULT '',
  tags TEXT[] NOT NULL DEFAULT '{}',
  recommended_action TEXT NOT NULL DEFAULT '',
  visibility TEXT NOT NULL DEFAULT 'public',
  revision_of TEXT REFERENCES match_events(id),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'corrected', 'deleted')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_match_events_match_created
  ON match_events(match_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_match_events_match_type
  ON match_events(match_id, event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_match_events_active
  ON match_events(match_id, created_at DESC)
  WHERE status = 'active';

CREATE TABLE IF NOT EXISTS event_participants (
  id BIGSERIAL PRIMARY KEY,
  event_id TEXT NOT NULL REFERENCES match_events(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  name TEXT NOT NULL,
  team_id TEXT NOT NULL DEFAULT '',
  team_name TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_event_participants_name
  ON event_participants(name, event_id);

CREATE TABLE IF NOT EXISTS conversation_turns (
  id BIGSERIAL PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('user', 'qiuqiu', 'event', 'system')),
  text TEXT NOT NULL,
  event_id TEXT REFERENCES match_events(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_conversation_turns_match_user_created
  ON conversation_turns(match_id, user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS agent_traces (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL DEFAULT '',
  input TEXT NOT NULL,
  intent TEXT NOT NULL,
  tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
  retrieved_event_ids TEXT[] NOT NULL DEFAULT '{}',
  output TEXT NOT NULL,
  reason TEXT NOT NULL,
  latency_ms INT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_traces_match_created
  ON agent_traces(match_id, created_at DESC);
