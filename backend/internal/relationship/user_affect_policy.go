package relationship

import (
	"strings"
)

// 用户语音情绪偏置（openspec/changes/policy-bits C2）。SenseVoice 情绪
// 信号已建（user-voice-affect 波1：sidecar 分类、relay 置信门、trace 合
// 并），此处是它唯一的行为消费者：话轮携带的叹气/低落信号 × 支持队落后
// 时，把战术问答（解说型 ActAnalyze）改道安慰型 ActReact。
//
// 只偏置不支配：改道只发生在解说型出口上，其余判定路径（边界/沉默/引用/
// 争执）原样；信号缺席（Affect 为 nil）时 applyUserAffectBias 恒不命中，
// 行为与现状逐字节一致。开关在调用方（QIUQIU_USER_AFFECT_POLICY_BIAS，
// 默认关）——关即载荷根本不上 signal，本文件无 config 依赖。

// UserAffectBias 是话轮携带的用户语音情绪偏置载荷。Label/Confidence 是
// relay 过门后的 sidecar 结论原样透传；TeamBehind 是调用方从快照+画像算
// 出的「支持队当前落后」事实——斜杠前的情绪、斜杠后的赛况在载荷上合流，
// 是否偏置由 policy 单点裁决。
type UserAffectBias struct {
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	TeamBehind bool    `json:"teamBehind"`
}

// UserAffectBiasMinConfidence 是偏置侧的置信门（policy-bits：0.55 门沿用）。
// relay 的落 trace 门是同一数值的另一处消费（观测取舍），两处各管各的门。
const UserAffectBiasMinConfidence = 0.55

// userAffectDejectedLabels 是「叹气/低落」标签集（sidecar 情绪词的闭集，
// 归一化小写后精确匹配）。未知标签不偏置——只偏置不支配的另一半。
var userAffectDejectedLabels = map[string]struct{}{
	"sad":     {},
	"sigh":    {},
	"low":     {},
	"fearful": {},
}

// UserAffectDejected 报告情绪标签是否落在叹气/低落集合内（词表单源，
// 调用方用它决定是否装配载荷，policy 用它裁决是否偏置）。
func UserAffectDejected(label string) bool {
	_, ok := userAffectDejectedLabels[strings.ToLower(strings.TrimSpace(label))]
	return ok
}

// applyUserAffectBias 在战术问答出口上裁决偏置：命中返回安慰型 ActReact
// 与理由码（替代 explicit_analysis_request），未命中返回空（调用方走现
// 状）。命中条件四取齐：信号在场、标签低落、置信过门、支持队落后。
func applyUserAffectBias(signal Signal) []string {
	if signal.User == nil || signal.User.Affect == nil {
		return nil
	}
	affect := signal.User.Affect
	if !UserAffectDejected(affect.Label) {
		return nil
	}
	if affect.Confidence < UserAffectBiasMinConfidence {
		return nil
	}
	if !affect.TeamBehind {
		return nil
	}
	return []string{"user_affect:comfort_over_analysis"}
}
