# Tasks: 评估工具

- [x] 9.1 UTMOSv2 工具:scripts/utmos/(独立 venv)——mos_rank.py 批量 MOS+排序 CSV、requirements.txt(torch 源注记)、README(盲测衔接流程+域差注记)。**venv 建立与 ≥10 样本实跑待有卡机器/盲测开测时执行**(开发机 python=Store 桩、无卡跑 torch 无意义,与韵律盲测门同触发条件;README 状态节已登记)。
- [x] 9.2 promptfoo CI 门禁:evals.yml 新增 `promptfoo-gate` job(独立于 pr-evals,起 eval backend 后跑 gate config,失败即红阻断);`scripts/promptfoo/run-gate.mjs` 本地/CI 同一入口。**注错验红已本地过**:正确比分断言改为 2-0 后 1 failed + exit=100(阻断生效),已恢复。
- [x] 9.3 对话级用例:`scripts/promptfoo/scenario.mjs`(WS 全链场景 provider,自含 seed:REST 注入 config/lifecycle/事件→WS 话轮→收集)+ gate config 四用例——错比分负例(seed 1-0 问比分必报对)、红队对抗(诱导报 2-0 不得附和,须给账本事实或否认)、让路顺序(事件先入用户话轮紧随,用户回复先于主动回复且主动不丢——「窗口内零下发」严格形态留 Go scheduler 套件,这边锁端到端顺序,spec 措辞差异已声明)。**断言全确定性 javascript,无需 judge key**;红队对抗集即「诱导报错比分」宪法负例。
- [x] 9.4 HTTP provider:promptfoo 原生 http 形态直评本地 Go 服务 REST 面(catalog 端点+形状断言,startEvalBackend 同形环境)——跑通。**边界如实记录**:REST 无文本话轮端点(conversation 仅 WS),对话级负例走 WS 场景 provider;HTTP provider 锁「服务在场+目录形状」防假通过。
- [x] 9.5 解说评测集骨架:docs/evals/commentary-eval-set.md——指标组合(BERTScore+球员实体/比分演进/事件类型/幻觉率,三硬门纪律)+JSONL 样本格式(candidate 必须全链产物)+采集门槛(≥50 样本三类覆盖才首跑);样本积累挂 auto-hosting 流量。

## 顺手修复(同 change)

- **观测层 config 的 YAML 本身是坏的(存量)**:promptfooconfig.yaml 的 `id: exec: ...` 裸写法与 `{{...}}` 裸值从未被 js-yaml 接受(promptfoo-observability 落地时只在 promptfoo view 形态验证过?本地实跑即崩)——已修(引号包裹),观测层 config 现可实跑。
- exec provider 的 cwd= config 文件目录(tests/promptfoo),脚本路径改 `../../scripts/...`(观测层同修);provider id 内 vars 不渲染(prompt 文本变量才是插值面)、http id 嵌 env default 过滤会炸渲染——两坑已绕开并在 config 注释。

## 状态与判据

- gate 本地全绿 4/4(score-question 的真实全链回复「现在是西班牙 1-0 德国」被确定性断言钉住);注错验红过(exit=100)。
- CI:evals.yml promptfoo-gate job,推后看 Actions 首跑(工具链备忘:GitHub 443 间歇断)。
- UTMOSv2 实跑与相关性记录、解说评测集样本与首跑——均挂用户侧素材触发点(见各文件状态节)。

## Sequencing

波3,独立可先行(9.1/9.2 不依赖任何 change)。9.5 的样本积累依赖 auto-hosting(波1B)。
