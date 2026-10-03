# Tasks: 评估工具

- [ ] 9.1 UTMOSv2 工具:scripts/ Python(独立 venv)批量 MOS+排序;与盲测流程衔接文档。
- [ ] 9.2 promptfoo CI 门禁:GitHub Action + 失败阻断;注错验证 Action 红。
- [ ] 9.3 对话级用例:陪看整段(含主动话轮)llm-rubric 断言 + 抢话/错比分负例 + 红队对抗集(「诱导报错比分」)。
- [ ] 9.4 HTTP provider:直评本地 Go 服务(startEvalBackend 同形环境)全链跑通。
- [ ] 9.5 解说评测集骨架:指标组合(BERTScore+实体正确率)+样本格式;样本积累挂 auto-hosting 流量。

## Sequencing

波3,独立可先行(9.1/9.2 不依赖任何 change)。9.5 的样本积累依赖 auto-hosting(波1B)。GitHub push 依赖 443 恢复(工具链备忘)——CI 门禁本地验证先行,push 后生效。
