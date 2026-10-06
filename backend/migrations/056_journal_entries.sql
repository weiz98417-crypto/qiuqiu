-- 球友手记（openspec/changes/teammate-journal）：reflection 产能的消费面。
-- 每用户每场一篇（(user_id, match_id) 唯一）；比分/队名/进球是生成时从
-- 账本终场投影定格的快照（手记引用账本事实，不是手记自己的事实——手记
-- 永不产生新的比赛事实）；goals/sources JSONB 字符串数组（溯源面：每句
-- 可追到账本行或画像条目）；liked 是用户态；用户删除=物理删（隐私生命
-- 周期，moments 同款）。
CREATE TABLE IF NOT EXISTS journal_entries (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    match_id TEXT NOT NULL,
    home_team TEXT NOT NULL DEFAULT '',
    away_team TEXT NOT NULL DEFAULT '',
    score TEXT NOT NULL DEFAULT '',
    goals JSONB NOT NULL DEFAULT '[]'::jsonb,
    body TEXT NOT NULL,
    season TEXT NOT NULL DEFAULT '',
    liked BOOLEAN NOT NULL DEFAULT FALSE,
    sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, match_id)
);
CREATE INDEX IF NOT EXISTS journal_entries_user_created ON journal_entries (user_id, created_at DESC);
