// local_health.go 承载本地 TTS 腿的健康探测（tts-supply-switch 7.2）：
// 后台周期试合成一句短句，连续通过才把「本地引擎可选」的门亮开——门控
// 的是运营台的三态设置（7.3 写入校验），不是运行中调用（运行中失败由
// 三态各自的回退语义接管）。失败原因留存呈现；探测永不 panic、永不阻塞
// 合成主路。
package tts

import (
	"context"
	"sync"
	"time"
)

// DefaultProbeInterval 是探测周期默认值：30s 一次试合成（CPU 亚秒/GPU
// 更快），QIUQIU_TTS_LOCAL_PROBE_SECONDS 可调。
const DefaultProbeInterval = 30 * time.Second

// probeConsecutivePasses 是点亮「可选」门所需的连续通过次数：一次通过
// 可能是模型刚加载完的窗口，连续通过才算稳。
const probeConsecutivePasses = 2

// LocalProbe 是本地腿健康探测循环。零值不可用；Start 起后台循环，
// Close 停。available 翻转只由探测结论驱动，读取无锁（atomic 语义由
// mu 保护的快照值承担）。
type LocalProbe struct {
	client    *LocalClient
	interval  time.Duration
	timeout   time.Duration
	closeCtx  context.Context
	close     context.CancelFunc
	closeOnce sync.Once

	mu         sync.Mutex
	available  bool
	reason     string
	streak     int
	lastAt     time.Time
	hasLastAt  bool
	probes     int64
	failures   int64
	notify     func(available bool, reason string)
}

// NewLocalProbe 构造探测循环；client 未配置或 interval 非正返回 nil
// （本地腿不存在即探测整体消失，与旁路组件同纪律）。
func NewLocalProbe(client *LocalClient, interval, timeout time.Duration) *LocalProbe {
	if client == nil || !client.Configured() || interval <= 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &LocalProbe{
		client:   client,
		interval: interval,
		timeout:  timeout,
		closeCtx: ctx,
		close:    cancel,
	}
}

// OnStateChange 注册状态翻转回调（7.3 的运行中告警接线点；nil 安全）。
func (p *LocalProbe) OnStateChange(fn func(available bool, reason string)) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.notify = fn
	p.mu.Unlock()
}

// Start 起后台探测；立刻先探一轮（启动即有状态，不用等第一个周期）。
func (p *LocalProbe) Start() {
	if p == nil {
		return
	}
	go p.loop()
}

// Close 停止探测循环。
func (p *LocalProbe) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() { p.close() })
}

func (p *LocalProbe) loop() {
	p.probeOnce()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.closeCtx.Done():
			return
		case <-ticker.C:
			p.probeOnce()
		}
	}
}

func (p *LocalProbe) probeOnce() {
	ctx, cancel := context.WithTimeout(p.closeCtx, p.timeout)
	defer cancel()
	err := p.client.Probe(ctx)

	p.mu.Lock()
	p.probes++
	p.lastAt = time.Now()
	p.hasLastAt = true
	previous := p.available
	if err != nil {
		p.failures++
		p.streak = 0
		p.available = false
		p.reason = err.Error()
	} else {
		p.streak++
		if p.streak >= probeConsecutivePasses {
			p.available = true
			p.reason = ""
		} else if !p.available {
			p.reason = "等待连续通过确认"
		}
	}
	flipped := previous != p.available
	notify := p.notify
	p.mu.Unlock()

	if flipped && notify != nil {
		notify(p.available, p.reason)
	}
}

// Snapshot 是运营台状态面的只读快照。
type LocalProbeSnapshot struct {
	Configured  bool   `json:"configured"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	LastCheckAt string `json:"lastCheckAt,omitempty"`
	Probes      int64  `json:"probes"`
	Failures    int64  `json:"failures"`
}

// Snapshot 返回当前健康快照；nil 探测返回 Configured=false（本地腿未
// 配置，运营台置灰并显示原因）。
func (p *LocalProbe) Snapshot() LocalProbeSnapshot {
	if p == nil {
		return LocalProbeSnapshot{Configured: false, Reason: "本地引擎未配置（QIUQIU_TTS_LOCAL_URL 留空）"}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := LocalProbeSnapshot{
		Configured: true,
		Available:  p.available,
		Reason:     p.reason,
		Probes:     p.probes,
		Failures:   p.failures,
	}
	if p.hasLastAt {
		snapshot.LastCheckAt = p.lastAt.UTC().Format(time.RFC3339)
	}
	return snapshot
}

// Available 报告当前门控状态（7.3 写入校验用）。
func (p *LocalProbe) Available() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available
}
