-- 赛后复盘邀约（openspec/changes/proactive-match-nodes）：提醒类别列。
-- 空字符串 = 既有赛前提醒（原语义零漂移）；fulltime_review = 终场复盘。
ALTER TABLE proactive_reminders ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT '';
