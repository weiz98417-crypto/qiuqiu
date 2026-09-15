---
lesson: 5
title: 表演层：从 ResponsePlan 到 Live2D
trace_position: trace 第 6 站。上游：Director Decision 的另一半——第 4 讲 realizes 台词，本讲执行表演；下游：评测层验证表演包真的被客户端执行（第 6 讲）
depends_on: [32-emotion-presentation, 33-immersive-stage]
---
# 第 5 讲 · 表演层：从 ResponsePlan 到 Live2D

## 这一站在 trace 上

佩德里进球触发 `SignalMatchEvent`，Director 产出 Decision：一半是台词（上一讲），一半是表演。表演的强度上限完全由后端决定，客户端只做白名单校验、别名映射和到点回落，不生成任何自发动作，进球时是 excited/cheer/2600ms。全程没有第二套表达词汇的生存空间——ADR-0005 删掉了想当第二套的引擎。

```
affect(goal: arousal +0.8) ─> presentationFor ─> PresentationPlan ─> WS type:"presentation"
      affect.go:13-17           affect.go:69      types.go:351       main.go:591
Live2dView ◄─ match_screen.dart:346 白名单+别名 ◄──────────────────┘
   └► exprMap / playMotion(cheer→celebrate) / setSpeaking(ParamJawOpen 抖动)
```

## 证据锚点

- `backend/internal/relationship/types.go:351-360` — PresentationPlan：8 字段唯一表演合同；Decision 内嵌于 types.go:254，director.go:101 组装
- `backend/internal/relationship/affect.go:69-123` — presentationFor：goal→excited/cheer（106-110）、要安静→low/idle（80-87）；`backend/internal/companion/agent.go:491-500` observationPresentation：确认→excited/cheer/VoiceEnergy 0.9/1800ms
- `backend/cmd/server/main.go:591-599` — 只在 Expression 非空时才推 `type:"presentation"`
- `client/lib/services/presentation_state.dart:2-51` — 白名单：13 表情、7 动作、8 语音风格、4 回落模式；17-35 别名表 deflated→sad、slump→idle、hold→focus、settle→idle、low→sad、tense→nervous
- `client/lib/screens/match_screen.dart:346-373` — `presentation` 消息与 legacy `expression` 通道都过白名单；941-949 hold 到期回落
- `client/lib/widgets/live2d_view.dart:7-8` — 条件导入桥：`live2d_bridge_stub.dart` if (dart.library.html) `live2d_bridge_web.dart`——桥在 widgets/，不在 services/
- `client/lib/widgets/live2d_view.dart:324,358-385` — exprMap 13→7 表情索引、cheer→celebrate、口型=50ms 随机 ParamJawOpen
- `client/lib/widgets/live2d_bridge_web.dart:36-66` — web 桥：postMessage 注入 iframe + pointerEvents=none
- `docs/adr/0005-presentation-single-contract.md:7-9` — 决定：PresentationPlan 白名单是唯一合同

## 代码走读

### 1. 唯一合同（backend/internal/relationship/types.go:351-360）

```go
type PresentationPlan struct {
	Affect      AffectState `json:"affect"`
	Expression  string      `json:"expression"`
	Motion      string      `json:"motion"`
	VoiceStyle  string      `json:"voiceStyle"`
	VoiceEnergy float64     `json:"voiceEnergy"`
	VoiceSpeed  float64     `json:"voiceSpeed"`
	HoldMS      int         `json:"holdMs"`
	ReturnMode  string      `json:"returnMode"`
}
```

后端随每个 Decision 产出这份计划。8 个字段就是表演的全部 vocabulary——多了没有，这是"合同"的含义。

### 2. 事件→表演映射（backend/internal/relationship/affect.go:105-123）

```go
	switch signal.Match.EventType {
	case "goal":
		plan.Expression = "excited"
		plan.Motion = "cheer"
		plan.VoiceStyle = "excited"
		plan.HoldMS = 2600
	case "var_check":
		plan.Expression = "tense"
		plan.Motion = "hold"
		plan.VoiceStyle = "tense"
		plan.HoldMS = 2200
	case "goal_cancelled":
		plan.Expression = "deflated"
		plan.Motion = "settle"
		plan.VoiceStyle = "low_disappointed"
		plan.HoldMS = 2800
	}
	return plan
}
```

基线在 69-79 行：energy=clamp(0.35+arousal*0.55)、speed=clamp(0.9+arousal*0.15)，goal 先把 arousal 抬 0.8（13-17 行）。注意这里发的全是"旧词汇"（tense/hold/deflated/settle）——后端没改口，改口责任交给客户端别名表。

### 3. 客户端白名单+别名（client/lib/services/presentation_state.dart:92-104）

```dart
    final rawExpression = presentation['expression'] as String? ?? '';
    final rawMotion = presentation['motion'] as String? ?? '';
    final expression = normalizeExpression(rawExpression);
    final motion = _motionAliases[rawMotion] ?? rawMotion;
    final voiceStyle = presentation['voiceStyle'] as String? ?? 'natural';
    final returnMode =
        presentation['returnMode'] as String? ?? 'decay_to_focus';
    if (expression == null ||
        !_allowedMotions.contains(motion) ||
        !_allowedVoiceStyles.contains(voiceStyle) ||
        !_allowedReturnModes.contains(returnMode)) {
      return null;
    }
```

任一字段不在白名单，整个表演包丢弃（null），不是"尽量演"。别名表吸收后端旧词汇：后端发 deflated，客户端演 sad。holdMs 被钳制在 0..10000（112 行），到期按 returnMode 回落 resting 状态，且仅当表演包未被更新者替换（match_screen.dart:946 的 identical 校验）。

### 4. 口型真相：随机抖动，不是逐音素同步（client/lib/widgets/live2d_view.dart:375-385）

```javascript
setInterval(function() {
    if (!model) return;
    if (speaking) {
        mouthValue = 0.25 + Math.random() * 0.75;
    } else {
        mouthValue += (0 - mouthValue) * 0.25;
    }
    try {
        model.internalModel.coreModel.setParameterValueById('ParamJawOpen', mouthValue);
    } catch(e) {}
}, 50);
```

`setSpeaking`（370-373 行）只切布尔，停说时 mouthValue 归零。动作/表情到 Cubism 是静态表：365 行 playMotion 把 cheer 映到 celebrate 动作组，324 行 exprMap 把 13 个表情映到 7 个索引、未知回退 0。所谓"口型同步"只是说话时每 50ms 给 ParamJawOpen 一次 0.25..1.0 随机值——远看像在说话，零延迟预算。

### 5. 平台桥：一个函数对，两种通道（无节选，给结论）

native 端 Live2dViewState 直接 `evaluateJavascript("setExpression(...)")`（live2d_view.dart:124-142）；web 端 iframe 拿不到句柄，`sendLive2dState`（live2d_bridge_web.dart:36-66）把状态 postMessage 进 `/live2d.html`，并顺手把 iframe 的 pointerEvents 置 none、外层再包 IgnorePointer（live2d_view.dart:158）——角色是背景，永远不抢控制权。选哪个桥由 live2d_view.dart:7-8 的条件导入在编译期决定。

## 评测联动

- Flutter：`cd client && flutter test test/reply_display_test.dart`。7 个用例锁合同：`maps supported backend presentation aliases safely`（deflated/settle→sad/idle）、`rejects unknown presentation commands`（expression 塞 `javascript:alert` 必须整体拒绝）、`presentation return mode chooses the planned resting state`。
- Go：`cd backend && go test ./internal/relationship -run 'TestDirectorPlansPresentationForGreetingAndUserReply|TestHumanityScenarioConformance' -v -count=1`。前者断言问候→happy/hello、用户回合→chat/speak；后者场景 `16_user_needs_quiet_after_loss` 断言"不想分析，陪我缓会儿"→ActSilence+expression=low。再加 `go test ./internal/companion -run TestConfirmedObservationCreatesGroundedFollowUp -v` 验证观察确认→excited/cheer。
- 失效时看到什么：把 goal 分支的 "excited" 改成白名单外的 "ecstatic"，fromReplyData 返回 null，进球时球球纹丝不动；删别名表一项，对应 Flutter 用例变红。`tests/evals/client-delivery-dedupe.spec.mjs:98-111` 向客户端注入 `type:"presentation"`（excited/cheer），134 行 poll 页面真的执行了 `cheer`、`settle`——表演链路断了这个数就凑不齐 2。

## 动手作业

1. 基线（应全绿）：`cd client && flutter test test/reply_display_test.dart`
2. 打开 `client/lib/services/presentation_state.dart`，删掉 `_expressionAliases` 里的 `'deflated': 'sad',`（20 行），重跑第 1 步。预期：`legacy expressions use the same safety whitelist`（19 行断言 normalizeExpression('deflated')=='sad'）和 `maps supported backend presentation aliases safely` 失败——deflated 不在 13 个白名单表情里，normalizeExpression 返回 null。改回后确认恢复绿色。
3. 盲区实验：把 `backend/internal/relationship/affect.go:110` 的 `plan.HoldMS = 2600` 改成 10000，跑 `cd backend && go test ./internal/relationship -run TestDirectorPlansPresentationForGreetingAndUserReply -v -count=1`——它不检查 HoldMS，照样绿。想想这个盲区该由哪层测试补，然后改回 2600。
4. 结论一句话：表演词汇由后端定，合法性由客户端验，两边各有测试把门；数值字段目前两边都没锁。

## 延伸

- 事实源章节：`docs/球球课程/chapters/32-emotion-presentation.facts.json`（合同/映射/白名单均 implemented-at，旧引擎标 concept）、`33-immersive-stage.facts.json`（非交互舞台、静态映射、随机口型）。
- 必读 ADR：`docs/adr/0005-presentation-single-contract.md`——为什么删 expression/engine.go 死代码、为什么拒绝第二套词汇；本讲所有"只有一条合同"的断言源于此。叙事呼应见 `docs/直播课-15张架构图逐字稿-非技术版.md` 图 11。
