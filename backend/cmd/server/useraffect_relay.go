package main

// 用户语音情绪旁路接线器（openspec/changes/user-voice-affect 波1）：在话轮
// 终稿处把整段 PCM 旁送给 voice-input sidecar（SenseVoice），结论经
// AttachVoiceStages 原子合并进该话轮的语音 trace（Voice.UserAffect）。
// 旁路纪律与 ambient_relay 同族：不阻塞 ASR/话轮主路；失败静默丢弃+计数；
// sidecar 摘除即旁路整体消失。宪法线：情绪信号永不进比赛事实账本——本
// 接线器的依赖面只有 classifier 与 agent.AttachVoiceStages（观测合并通道），
// 结构上不存在事实写路径（TestUserVoiceAffectNeverEntersTheFactLedger）。
//
// 时序两个方向都成立：结论先于话轮投递完成 → 挂点即时取走；后于 → relay
// 协程按 bind 时登记的 traceID 直接事后合并（代次=utterance 的 signalID，
// 迟到的旧 signalID 结论在连接内天然孤立，连接拆除即整体消失）。

import (
	"context"
	"sync"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/useraffect"
)

// useraffectRelayMinConfidence 是置信门默认值：sidecar 伪置信低于门槛的
// 结论不落 trace（USER_AFFECT_MIN_CONFIDENCE 可配）。
const useraffectRelayMinConfidence = 0.55

// userAffectClassifier 是 relay 对 sidecar 客户端的最小依赖面；*useraffect.Client
// 实现之，测试用桩替换。
type userAffectClassifier interface {
	Classify(ctx context.Context, pcm []byte) (useraffect.Signal, error)
}

// voiceObservationAttacher 是 relay 对观测合并通道的最小依赖面；
// *companion.Agent 实现之（AttachVoiceStages）。
type voiceObservationAttacher interface {
	AttachVoiceStages(ctx context.Context, matchID, traceID string, patch companion.VoiceObservationPatch) error
}

// useraffectRelayMaxInFlight 在途上限：宁失明不反压主路（与 ambient 同值）。
const useraffectRelayMaxInFlight = 4

type useraffectRelay struct {
	classifier userAffectClassifier
	attacher   voiceObservationAttacher
	matchID    string
	// minConfidence 之下的结论静默丢弃。
	minConfidence float64
	dropped       chan struct{}
	inFlight      chan struct{}

	mu sync.Mutex
	// signals 与 waiters 二选一地 keyed by signalID：signal 先到则暂存等
	// 挂点取走；挂点先到则登记 traceID 等结论回来直接事后合并。
	signals map[string]useraffect.Signal
	waiters map[string]string
}

// newUserAffectRelay 装配旁路；classifier 为空返回 nil（旁路停用，等价于
// 未配置 USER_AFFECT_URL）。
func newUserAffectRelay(classifier userAffectClassifier, attacher voiceObservationAttacher, matchID string, minConfidence float64) *useraffectRelay {
	if classifier == nil || attacher == nil {
		return nil
	}
	if minConfidence <= 0 {
		minConfidence = useraffectRelayMinConfidence
	}
	return &useraffectRelay{
		classifier:    classifier,
		attacher:      attacher,
		matchID:       matchID,
		minConfidence: minConfidence,
		dropped:       make(chan struct{}, 1024),
		inFlight:      make(chan struct{}, useraffectRelayMaxInFlight),
		signals:       make(map[string]useraffect.Signal),
		waiters:       make(map[string]string),
	}
}

// Dropped 返回静默丢弃计数（sidecar 失败/置信门未过/在途饱和）。
func (r *useraffectRelay) Dropped() int {
	if r == nil {
		return 0
	}
	return len(r.dropped)
}

func (r *useraffectRelay) countDropped() {
	select {
	case r.dropped <- struct{}{}:
	default:
	}
}

// Forward 把一段话轮终稿 PCM 旁送给 sidecar：立即返回。PCM 归旁路所有。
func (r *useraffectRelay) Forward(userID, signalID string, pcm []byte) {
	if r == nil || len(pcm) == 0 || userID == "" || signalID == "" {
		return
	}
	select {
	case r.inFlight <- struct{}{}:
	default:
		r.countDropped()
		return
	}
	go r.classify(userID, signalID, append([]byte(nil), pcm...))
}

func (r *useraffectRelay) classify(userID, signalID string, pcm []byte) {
	defer func() { <-r.inFlight }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	signal, err := r.classifier.Classify(ctx, pcm)
	if err != nil {
		r.countDropped()
		return
	}
	if signal.Confidence < r.minConfidence {
		r.countDropped()
		return
	}
	r.mu.Lock()
	if traceID, waiting := r.waiters[signalID]; waiting {
		delete(r.waiters, signalID)
		r.mu.Unlock()
		r.attach(traceID, signal)
		return
	}
	r.signals[signalID] = signal
	r.mu.Unlock()
}

// bind 由话轮挂点调用：结论已到则即时合并，未到则登记 traceID 等结论回来
// 事后合并。无论哪种，本话轮至多一次合并。
func (r *useraffectRelay) bind(signalID, traceID string) {
	if r == nil || traceID == "" {
		return
	}
	r.mu.Lock()
	signal, ready := r.signals[signalID]
	if ready {
		delete(r.signals, signalID)
		r.mu.Unlock()
		r.attach(traceID, signal)
		return
	}
	r.waiters[signalID] = traceID
	r.mu.Unlock()
}

func (r *useraffectRelay) attach(traceID string, signal useraffect.Signal) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.attacher.AttachVoiceStages(ctx, r.matchID, traceID, companion.VoiceObservationPatch{
		UserAffect: &companion.UserAffectSignal{Label: signal.Label, Confidence: signal.Confidence},
	}); err != nil {
		r.countDropped()
	}
}
