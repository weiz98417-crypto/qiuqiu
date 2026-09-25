package main

// 气氛旁路接线器（openspec/changes/ambient-audio-observation 6.2）：在
// watchconnection 的 asr_chunk 处并行旁送音频分片给 AED sidecar，事件以
// kind=ambient 的旁证输入落 observation store。旁路三纪律（design.md）：
// 不阻塞 ASR 主路（Forward 只投递协程、立即返回）；失败静默丢弃+计数
// （不 log 不报错风暴）；sidecar 摘除即旁路整体消失，主链路无感。

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"qiuqiu/internal/ambient"
	"qiuqiu/internal/observation"
)

// ambientClassifier 是 relay 对 sidecar 客户端的最小依赖面；*ambient.Client
// 实现之，测试用桩替换。
type ambientClassifier interface {
	Classify(ctx context.Context, pcm []byte) ([]ambient.Event, error)
}

// ambientRelayMaxInFlight 是旁路在途上限：sidecar 健康但慢时，批量
// asr_chunk 的无上限投递会堆积协程把它打挂；饱和即丢弃，不反压主路。
const ambientRelayMaxInFlight = 4

// ambientRelay 是一条观赛连接的气氛旁路：nil 即旁路停用。
type ambientRelay struct {
	classifier  ambientClassifier
	coordinator observation.Coordinator
	// dropped 累计旁路静默丢弃的事件数（sidecar 失败整批计 1、落库失败
	// 逐条计、在途饱和整片计 1）。
	dropped atomic.Int64
	// 在途信号量（容量 ambientRelayMaxInFlight）：Forward 时同步占位，
	// deliver 返回时释放。
	inFlight chan struct{}
}

// newAmbientRelay 装配旁路；classifier 与 coordinator 任一为空则返回 nil
// （旁路停用，等价于摘除 sidecar 后的形态）。
func newAmbientRelay(classifier ambientClassifier, coordinator observation.Coordinator) *ambientRelay {
	if classifier == nil || coordinator == nil {
		return nil
	}
	return &ambientRelay{
		classifier:  classifier,
		coordinator: coordinator,
		inFlight:    make(chan struct{}, ambientRelayMaxInFlight),
	}
}

// Dropped 返回旁路静默丢弃计数（摘除场景测试与运维观测用）。
func (r *ambientRelay) Dropped() int64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

// Forward 把一个音频分片旁送给 sidecar：立即返回，判定与落库都在独立
// 协程里完成（分片字节数组归旁路所有，ASR 主路继续用自己那份）。
// 身份未识别或比赛为空时跳过——气氛旁证仅观赛会话内生效，且必须落在
// user+match 作用域内。在途超过 ambientRelayMaxInFlight 时整片静默丢弃
// 并计入 dropped（宁失明不反压）。
func (r *ambientRelay) Forward(userID, matchID string, pcm []byte) {
	if r == nil || len(pcm) == 0 || userID == "" || matchID == "" {
		return
	}
	select {
	case r.inFlight <- struct{}{}:
	default:
		r.dropped.Add(1)
		return
	}
	go r.deliver(userID, matchID, pcm)
}

// deliver 在旁路协程内完成「判定 → 旁证落库」，任何失败静默丢弃+计数。
// 用独立预算的 background context：连接拆除不打断在途的短调用，也不让
// 旁路反过来拖住连接生命周期。
func (r *ambientRelay) deliver(userID, matchID string, pcm []byte) {
	defer func() { <-r.inFlight }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, err := r.classifier.Classify(ctx, pcm)
	if err != nil {
		r.dropped.Add(1)
		return
	}
	for _, event := range events {
		receivedAt := event.TS
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}
		// signalID 按「种类+分钟桶」成槽：同一分钟同种类事件由
		// coordinator 的既有去重合并为一个旁证行，不挤占活跃观察额度。
		signalID := fmt.Sprintf("ambient:%s:%d", event.Kind, receivedAt.Unix()/60)
		if _, err := r.coordinator.Record(ctx, observation.AmbientCorroboration(signalID, userID, matchID, event.Kind, receivedAt)); err != nil {
			r.dropped.Add(1)
		}
	}
}

// relayAmbientChunk 是 watchconnection 在 asr_chunk 处的旁路挂点：校验
// 合法的音频分片原样旁送，nil relay（未配置端点）直接跳过。
func (c *watchConnection) relayAmbientChunk(pcm []byte) {
	if c.deps.ambient == nil {
		return
	}
	c.deps.ambient.Forward(c.identity.Get(), c.matchID, pcm)
}
