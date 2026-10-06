# Memobase 版本能力盘点（openspec/changes/memory-scoring 8.4）

> 结论先行：**适配器未用上 0.0.36/0.0.37/0.0.40 三个版本的任何新能力**，
> 但三项里只有「context API」值得接（召回组装省一层胶水）；gist 搜索与
> workflow 重写收益不足或不适用。镜像升级（mbcompose.json，本机 Docker
> 不可用）留用户侧；升级后跑全量记忆 evals 回归 + token 对照。

## 一、适配器现状（backend/internal/memory/memobase.go）

端点清单（对齐官方 Go SDK，stdlib 手写）：

| 端点 | 用途 | 消费方 |
| --- | --- | --- |
| `GET /healthcheck` | 存活探测 | health 面 |
| `GET/POST /users` | ensureUser | 全链 |
| `POST /blobs/insert/{id}?wait_process=b` | 话轮 blob 入库 | Observe 管线 |
| `POST /users/buffer/{id}/chat?wait_process=b` | flush 提取 | ReflectNow |
| `GET /users/profile/{id}?max_token_size&prefer_topics` | 画像条目 | Portrait / Recall(adapter 腿) |
| `PUT/DELETE /users/profile/{id}/{profileID}` | 条目修正 | C3 编辑面 |

打分：8.1 起两腿同代（relevance × importance × exp(-age/τ)），融合配额
语义不变。

## 二、三版本能力 vs 适配器

### 0.0.36 — context API（记忆直接打包进 prompt）

- **形态**：`/users/{id}/context` 一步返回组装好的记忆 prompt（替代
  「拉 profile 条目→自己拼块」）。
- **适配器现状**：`Recall` 自己做（profile 条目 → 三因子打分 → top-k →
  render）；`Portrait` 自己渲染有界中文块。
- **裁决：值得接，但非现在**。context API 的打包策略黑盒化，会把
  8.1 的三因子排序权交回服务端——与「排序可复现（evals 钉住）」冲突。
  可接入形态：仅 Portrait 渲染腿换 context API（观测面无排序诉求），
  Recall 腿保留本地打分。触发条件：Memobase 升级后的下一轮（先升级跑
  回归，再单独开小 change 接）。
- **token 收益**：省一层胶水调用（portrait 一次 GET），正链路召回本身
  无省——0.0.36 宣称的省来自调用方拼装成本，不是每话轮 token。

### 0.0.37 — event gist 细粒度搜索

- **形态**：blob 事件层按 gist 细粒度检索（超越 profile 摘要层）。
- **适配器现状**：无事件层检索——向量腿（pgvector+本地 bge-m3）已覆盖
  「按内容找历史瞬间」的同一需求，且自持（不依赖 Memobase 版本）。
- **裁决：不接**。同一能力已有更强的本地实现（向量腿+内容精确腿），
  再接 gist 搜索=第三条冗余召回腿，违背融合配额纪律。

### 0.0.40 — workflow 重写（LLM 调用固定 3 次，token -40~50%）

- **形态**：提取管线重写，每 blob 的 LLM 调用收敛固定 3 次。
- **适配器现状**：提取是黑盒（服务端行为），适配器只发 blob+flush。
- **裁决：随镜像升级自动享受，零适配器改动**。这是升级的主收益——
  token 对照记录（8.4 的回归项）升级前后各采一轮 Observe→flush 的
  Memobase 侧日志/token 计量（部署文档见 deploy/memobase/README）。

## 三、执行清单（用户侧，Docker 可用后）

1. `mbcompose.json` 镜像 tag 升至 ≥0.0.40，起服；
2. 回归：`go test ./internal/memory/...` 全量 + pr tier（evals 含记忆
   回放用例）；
3. token 对照：升级前后各跑一轮 demo-seed + 固定话轮脚本，记录
   Memobase 服务端日志的 LLM 调用次数/token 数，结论回填本档；
4. （可选，单独小 change）Portrait 腿接 context API，Recall 腿保持
   本地三因子。

## 状态

- [x] 能力盘点表（本档，8.4 盘点项交付）
- [ ] 镜像升级 + 回归 + token 对照（用户侧，Docker 不可用于开发机）
- [ ] context API 接入评估（升级后单独小 change）
