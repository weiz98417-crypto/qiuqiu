# Grafana 本机闭环运维（Windows 开发机，ADR-0021）

> 生产化（auth-proxy 反代统一鉴权 + docker compose）后置到部署轮；本机
> 闭环仅绑 127.0.0.1，不构成暴露面。2026-09-29 已实测跑通：六面板出数、
> console 观测页 kiosk 内嵌。

## 一次性安装

1. 下载 Grafana 13 OSS Windows 包（**文件名是横线分隔**：
   `grafana-13.2.2.windows-amd64.zip`；下载页默认勾选 Enterprise，必须
   手动切 OSS）：
   ```
   https://dl.grafana.com/oss/release/grafana-13.2.2.windows-amd64.zip
   ```
   解压到 `E:\tools\grafana`（bin/grafana.exe 即主程序；13 起
   grafana-server.exe 不再存在）。
2. 建只读数据源账号（Postgres 起着的状态下，psql 执行一次；init.sql 与
   ledger.yml 默认库名 qiuqiu_test——compose 部署若 POSTGRES_DB=qiuqiu，
   两处的库名与 GRANT 同步改）：
   ```
   psql -h 127.0.0.1 -U postgres -v qiuqiu_grafana_ro_password='<真实密码>' -f deploy/grafana/init.sql
   ```
   **注意时序**：GRANT 只对已存在的表生效——账本表由服务端迁移创建，
   首次建库后要在迁移跑完后再执行一次 init.sql 的 GRANT 段。
   账号仅 SELECT 四张账本表（agent_traces/interaction_ledger/
   delivery_ledger/proactive_reminders），物理上写不了账本（ADR-0021 决定 1）。

## 起停（conf 自定义，避免动安装目录）

`custom.ini`（放 `E:\tools\grafana\conf\custom.ini`，关键项）：

```ini
[server]
http_addr = 127.0.0.1
http_port = 3300

# 同源反代子路径（后端 /grafana/* 反代依赖这两项，缺了会 301 自环）
[server]
root_url = http://127.0.0.1:18090/grafana
serve_from_sub_path = true

[security]
allow_embedding = true        ; console 观测页 iframe 需要

[auth.anonymous]
enabled = true
org_role = Viewer             ; 只读；仅 127.0.0.1 可达

[paths]
provisioning = E:\数字人开发\qiuqiu\deploy\grafana\provisioning
data = E:\tools\grafana\data
logs = E:\tools\grafana\logs
plugins = E:\tools\grafana\plugins
```

启动（**13 起 server 是子命令，flags 跟在 server 后**；dashboard 目录走
环境变量插值，见 provisioning/dashboards/provider.yml）：

```
set GRAFANA_RO_PASSWORD=<真实密码>
set GRAFANA_DASHBOARDS_DIR=E:\数字人开发\qiuqiu\deploy\grafana\provisioning\dashboards
E:\tools\grafana\bin\grafana.exe server --homepath E:\tools\grafana --config E:\tools\grafana\conf\custom.ini
```

## 踩坑记录（2026-09-29 实测，重装必读）

- **datasource 必须显式写 `uid: qiuqiu-ledger`**：dashboard JSON 按 uid
  引用数据源，缺省时随机 uid → 全部面板查无此源。
- **`database` 必须写进 `jsonData.database`**：顶层 `database:` 是旧形
  态——后端健康检查照过（Connection OK），前端查询直接报「no default
  database」，全部面板 No data（真根因，前端 console.error 才见分晓）。
- dashboard provider 的 `path` 用 `${GRAFANA_DASHBOARDS_DIR}` 环境变量：
  POSIX 绝对路径在 Windows 被按当前盘符解析，相对路径按 cwd 解析，都不
  可靠。
- 面板 No data 且后端日志无查询请求时，钩 `console.error` 能看到被
  catchError 吞掉的前端错误——本例「no default database」只在这里可见。

## console 接入

后端读 `QIUQIU_GRAFANA_URL`（如 `http://127.0.0.1:3300`）：
- 已配置 → console NAV「观测」页 iframe 嵌
  `/d/qiuqiu-ops/?kiosk`（kiosk 模式隐藏 Grafana 外壳）。
- 未配置 → 部署指引占位卡（与 Operators 页 501 降级同模式）。

## 面板纪律（评审 checklist）

- SQL 只出聚合数字，**禁止任何正文列**（input/output/asr_text/phrase）。
- 改面板 = 改 `deploy/grafana/provisioning/dashboards/qiuqiu-operations.json`
  走 PR，UI 上手改只在验证阶段、改完导回仓库。
