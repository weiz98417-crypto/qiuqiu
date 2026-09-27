# Grafana 本机闭环运维（Windows 开发机，ADR-0021）

> 生产化（auth-proxy 反代统一鉴权 + docker compose）后置到部署轮；本机
> 闭环仅绑 127.0.0.1，不构成暴露面。

## 一次性安装

1. 下载 Grafana 13 OSS Windows 二进制（tar.gz）：
   https://grafana.com/grafana/download?platform=windows
   **下载页默认勾选 Enterprise，必须手动切 OSS**（许可证差异，AGPL）。
2. 解压到 `E:\tools\grafana`（bin/grafana-server.exe 即主程序）。
3. 建只读数据源账号（Postgres 起着的状态下，psql 执行一次；init.sql 与
   ledger.yml 默认库名 qiuqiu_test——compose 部署若 POSTGRES_DB=qiuqiu，
   两处的库名与 GRANT 同步改）：
   ```
   psql -h 127.0.0.1 -U postgres -v qiuqiu_grafana_ro_password='<真实密码>' -f deploy/grafana/init.sql
   ```
   账号仅 SELECT 四张账本表（agent_traces/interaction_ledger/delivery_ledger/
   proactive_reminders），物理上写不了账本（ADR-0021 决定 1）。

## 起停（conf 自定义，避免动安装目录）

`custom.ini`（放 `E:\tools\grafana\conf\custom.ini`，关键项）：

```ini
[server]
http_addr = 127.0.0.1
http_port = 3300

[security]
allow_embedding = true        ; console 观测页 iframe 需要

[auth.anonymous]
enabled = true
org_role = Viewer             ; 只读；仅 127.0.0.1 可达

[paths]
provisioning = <repo>/deploy/grafana/provisioning
data = E:\tools\grafana\data
logs = E:\tools\grafana\logs
```

启动：`E:\tools\grafana\bin\grafana-server.exe --homepath E:\tools\grafana --config E:\tools\grafana\conf\custom.ini`

环境变量 `GRAFANA_RO_PASSWORD` 传给 datasource provisioning（或直接改
ledger.yml 里的 `${GRAFANA_RO_PASSWORD}` 为真实值——本机闭环可接受）。

## console 接入

后端读 `QIUQIU_GRAFANA_URL`（如 `http://127.0.0.1:3300`）：
- 已配置 → console NAV「观测」页 iframe 嵌
  `/d/qiuqiu-ops/?kiosk`（kiosk 模式隐藏 Grafana 外壳）。
- 未配置 → 部署指引占位卡（与 Operators 页 501 降级同模式）。

## 面板纪律（评审 checklist）

- SQL 只出聚合数字，**禁止任何正文列**（input/output/asr_text/phrase）。
- 改面板 = 改 `deploy/grafana/provisioning/dashboards/qiuqiu-operations.json`
  走 PR，UI 上手改只在验证阶段、改完导回仓库。
