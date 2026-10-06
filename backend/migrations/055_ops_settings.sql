-- 运营全局设置（openspec/changes/tts-supply-switch）：部署级单值 KV——
-- 首行消费者是语音供给三态（key='tts_supply'，value ∈ cloud/local/
-- local_first）。全局单值不按比赛不按用户；写入走运营 API（幂等+审计），
-- 运行中即时生效。通用表形态是有意最小：后续部署级开关同表加行，
-- 不为每个开关建表。
CREATE TABLE IF NOT EXISTS ops_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
