# 用户语音情绪旁送:voice-input sidecar(波1)

## Why

情绪状态(Affect State)目前只从文本与比赛事实驱动,用户语音里的副语言信号——喊出来的激动、叹气、低落——全部丢弃;氛围 AED(SenseVoice)只听球场不听用户。asr_chunk 旁送基建现成(cmd/server/ambient_relay.go,不反压主路),sidecar 模式已两次验证(turn/ambient 两个 compose profile),SenseVoice-Small(234M,CPU 友好)输出情绪标签。新领域概念 **User Voice Affect(用户语音情绪)** 已入 CONTEXT.md——区别于球球自己的 Affect State。

## What Changes(波1:旁送与观测,不接政策)

- **voice-input sidecar**(compose profile `voice-input`):FastAPI + onnxruntime,与 turn-sidecar 同构;SenseVoice-Small(hf-mirror 下载、sha256 锁版本、E:\tools 先例);全 CPU,无常驻 GPU 依赖;对用户 asr_chunk 出情绪标签+置信。
- **WS 契约**:复用「既有 WS 新消息对」模式(turn_query/turn_result 先例)——旁送带 utteranceId,回 `affect_result {label, confidence, utteranceId}`;**代次绑定防陈旧结论**(f0b76ad 教训:RemoteTurnModel 迟到结论按旧文本提前截断),迟到丢弃;relay 失败静默计数不反压主路(降级即隐身,ambient 纪律)。
- **信号语义**:话轮聚合(话轮内主标签+平均置信,话轮级落一次,不是每个 partial 都记)+置信门(低于阈值不落);落 trace `user_affect` 键(trace-genai-alignment 字段体系);运营台单轮回放显形情绪标签列(三层隐私纪律照旧——标签无正文)。
- **宪法线负例测试(波1 就立)**:用户情绪永不进比赛事实账本——与球场气氛同规格。
- 波2(情绪偏置进球球 Affect State,0.1 量级、过全部限制门后再施加——4d25922 过门序教训)在观测数据确认标签质量后另立 change;本 change 不接任何政策。

## User Stories

1. As a 球球, I want 感知用户说话时的情绪(波2 才用), so that 后续回合能被用户情绪带动。
2. As a 运营, I want 情绪标签在观测台可见, so that 先验证信号质量再决定接政策。

## Non-goals

- 波2 情绪偏置进 Affect State / 政策(观测先行,另立 change)。
- ASR 替换 / FunASR(asr-selfhost-eval 管)。
- 情绪进 observation coordinator(用户情绪是关系域信号,不是比赛观察旁证——概念上归 User Voice Affect,不归 Match Fact 旁证体系)。
- 情绪改变任何确定性路径(无信号=现状)。
- 每段音频的逐 partial 情绪记录(只记话轮聚合)。

## Success Criteria

- 宪法负例:情绪路径对事实账本零写入(测试锁);
- 旁送不阻塞主路(语音延迟分解无回归);
- 陈旧结论丢弃(代次用例);
- eval:sidecar 缺席=现状(静默降级路径);
- compose profile 隔离(b2d4601 教训:profile 不互相拖起)。
