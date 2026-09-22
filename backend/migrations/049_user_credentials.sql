-- 客户端登录凭证（openspec/changes/login-credential-seam，ADR-0020）：
-- 邮箱+密码镜像运营端模式。绑定不换 ID——登录成功即给现有 usr_* 原地
-- 附加凭证，画像/记忆/订阅/关系（全部以 user_id 为主键）零迁移。
CREATE TABLE IF NOT EXISTS user_credentials (
  user_id TEXT PRIMARY KEY,
  identifier TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_credentials_identifier
  ON user_credentials(identifier);
