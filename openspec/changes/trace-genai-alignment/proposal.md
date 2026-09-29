# Trace GenAI 语义对齐:命名收敛与常量集中

## Why

自建 trace(五段语音延迟分解、事后 attach 原子合并落 PG、traceID 贯穿)是领域资产,但字段命名是私有词汇。生态三端(Langfuse/Phoenix/OpenLLMetry)正收敛到 OTel GenAI 语义约定(`gen_ai.*`,已迁独立仓库 open-telemetry/semantic-conventions-genai,未 stable 但方向已定)。不对齐,未来向任何 OTLP 后端双写都要重映射;对齐成本在当前只是一次改名+常量集中——消费面仅 Grafana 六面板与 console 回放页,自产自销,无外部消费者。

## What Changes

- trace 字段常量收敛到单一包(现散在 companion/trace.go、companion/voice_stages.go、observation 的各 WriteTrace 构造点),一次性对齐 `gen_ai.*`:LLM 调用类属性(model/system/token/finish_reason 等)按约定命名。
- **语音段锚点保留领域名**(asr_start/asr_finish/turn_decided 等现有锚点及 voice-streaming-delivery 将新增的 tts_first_audio)——语音段约定生态未 stable,不为对齐而对齐;design 文档记录对齐清单/保留清单/理由。
- Grafana 六面板查询语句与 provisioning 同步改+golden 重锁(768c7c5 先例);console TurnReplay 页字段同步——**同一提交内改完**(面板与 trace 字段不同步=grafana-alerting 的口径一致性隐患)。
- 不引入 Go OTel SDK、不改存储、不改 attach 体系——命名对齐,不是观测体系重建。
- 与 mcp-registry-serve 的交叉约定:工具调用 trace 字段直接按 `gen_ai.tool.*` 定名,一次到位避免二次改名。

## User Stories

1. As a 运营/调试者, I want trace 字段名与生态约定一致, so that 未来接 Phoenix 或任何 OTLP 后端不用重映射。
2. As a 面板维护者, I want 字段常量集中一处, so that 后续改名只动一个包。

## Non-goals

- 引入 OTel SDK / span 树 / OTLP 导出(观测体系重建,领域资产「五段延迟+原子合并」会被通用形态重写,收益不成立)。
- 起 Phoenix 试接(演示环境无真实负载,没数据可看;留记录:生产流量出现时单容器首选)。
- 语音段锚点改名(保留领域名,等约定 stable 再评估)。
- 新旧字段双名过渡期(自产自销,同步改完即锁)。

## Success Criteria

- trace payload 新字段名的 eval 断言(防改名漂移);
- Grafana 面板 golden 重锁、console 回放字段同步验证;
- design 文档落 docs/design(对齐清单/保留清单/理由);
- go 全量绿、pr tier 绿。
