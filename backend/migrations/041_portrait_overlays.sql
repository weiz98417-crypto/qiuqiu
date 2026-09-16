-- C3 球球懂我: local override layer for the synthesized user portrait.
--
-- Design decision (documented choice per the agent-depth plan): Memobase
-- profile slots CAN be edited/deleted via its API, but its extraction may
-- re-synthesize a deleted fact from old blobs and its profile reads are
-- cached, so a remote-only delete is not immediately durable. User edits and
-- forget requests are therefore written HERE first — this table is the
-- authority for what the next turn sees — and then forwarded best-effort to
-- Memobase (PUT/DELETE /users/profile/{id}/{profileID}) so the synthesis
-- layer converges when reachable.
--
-- Every read and write honors the privacy lifecycle (privacy.CheckDeletion),
-- and the full-account deletion job removes rows for the deleted user.

CREATE TABLE IF NOT EXISTS portrait_overlays (
  user_id TEXT NOT NULL,
  topic TEXT NOT NULL,
  sub_topic TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  deleted BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, topic, sub_topic)
);

CREATE INDEX IF NOT EXISTS idx_portrait_overlays_user
  ON portrait_overlays(user_id);
