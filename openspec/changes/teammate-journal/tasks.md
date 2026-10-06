# Tasks: 球友手记

- [x] 10.0 前置核查:分享卡片 MVP 存在性——**不存在**(client/lib 零「分享卡片渲染面」命中;moments_service.dart 是共同瞬间 API 客户端,非分享面,初查措辞已修正)。结论:10.4 含最小分享渲染面(文本卡片+系统分享,share_plus 已在依赖)。
- [x] 10.1 手记生成:`internal/journal` 包——Generate(账本终场投影 MatchFacts+画像条目素材 → LLM 织写(LLMGenerator,llm seam)→ **确定性比分后校验**(validateBody:正文比分形态必须与账本终场一致)→ 不一致/无 generator/失败降级 DeterministicDraft 确定性底稿——手记面永远有内容且零编造);sources 溯源面(match:/goal:/portrait: 引用);幂等(同 user+match 一篇,重复 beat 更新);挂点=runReflectionBeat post_match 分支(journalService.afterPostMatch,素材门:0-0 且无事件=不生成,防把空账本写成闷平);生成尽力而为失败只记日志。**evals**:零编造四分支(织写错比分降级/正确保留/无 generator/失败)+溯源抽查+幂等+素材门(cmd/server journal_api_test)。
- [x] 10.2 手记面:Store 接口(PG migration 056 journal_entries(user_id,match_id 唯一)+Memory 兜底)+ `/api/me/journal`(GET 列表/PUT {id}/like/DELETE {id} 物理删)+鉴权口径与 moments 一致(session bearer+ScopeUserRead;无库=空列表静默)。**路由注册锁**:TestJournalRoutesLocked 锁 main.go **四条**注册行(含 like——装配脚本曾漏注册该行,被本测试当场抓住,CRLF 吞注册教训复利);API 端到端经 ServeMux 直调(PathValue 只在 mux 分发下有值)。
- [x] 10.3 赛季记忆册:BuildAlbum 装订(每场一条:比赛快照+手记摘要[首句 ≤60 字]+共同瞬间[Moment 投影按场过滤,queueMomentSource 适配器避免 journal 反向依赖 memory])+season 过滤+GET /api/me/journal/album?season=。赛季标签=MatchConfig.Kickoff 年份(缺失退当前年)。**「赛季末一键成册」语义收敛(审查登记)**:读时投影(装配视图)即最小成册——无独立成册动作/无持久册页,属可辩护 MVP 弱化,独立成册留尾;**装订完整性口径**:相对已生成手记成立(0-0 素材门/生成失败的场会缺席 album),结构性无重复((user_id,match_id) 唯一)。
- [x] 10.4 客户端:journal_service.dart(fetch/like/forget/album,moments 同形态)+journal_screen.dart(手记列表卡:比分+正文+进球+点赞[记下这篇/记着呢]+分享+忘掉;赛季册 tab;SegmentedButton 切换)+入口=PortraitScreen 可选 onOpenJournal(「球友手记·赛季记忆册」按钮,null=隐藏零波及)+match_screen._openJournal 注入(同 session,无比赛连接可用)。分享=最小渲染面:文本卡片经 share_plus 系统分享(10.0 结论)。
- [x] 10.5 门禁:go 全量 34 包绿(+journal 包)/dart analyze 零 error/flutter test 241 绿。**真机三动作(读/赞/分享)=用户侧待办**(分享面板为系统 UI,模拟器不可验)。

## Sequencing

波3,依赖 memory-surfacing 落地(素材面)✓。软依赖 memory-scoring 的 dry-run(手记质量)✓(8.3 已落)。B4 挂起记录在 proposal Non-goals(锚点=第一批真实陪看用户)。
