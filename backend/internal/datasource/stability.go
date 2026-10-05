package datasource

// 稳定窗自动确认(auto-hosting 2.3;ADR-0024):provisional 事实在窗内无上游
// 漂移、不在 open 冲突中、事件类在信任表白名单 → 自动确认,操作者显名
// auto: 前缀。确认失败只记日志(下轮重见重试),冲突挂起永不自动裁决。

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/matchstate"
)

// autoConfirmableEventTypes 是信任表白名单(ADR-0024,default-deny):VAR 类
// /改判类/点球类/fulltime 及其余一切类型永远留运营。改表须过评审。
var autoConfirmableEventTypes = map[string]bool{
	"goal":         true,
	"red_card":     true,
	"yellow_card":  true,
	"substitution": true,
	"kickoff":      true,
	"halftime":     true,
	"fulltime":     true,
}

const autoConfirmOperatorID = "auto:stability"

const stabilitySweepInterval = 5 * time.Second

type stabilityMark struct {
	at       time.Time
	driftSeq int64
}

type stabilityWindowState struct {
	mu        sync.Mutex
	firstSeen map[string]stabilityMark
}

func newStabilityWindowState() *stabilityWindowState {
	return &stabilityWindowState{firstSeen: make(map[string]stabilityMark)}
}

func (w *stabilityWindowState) markAt(key string) (stabilityMark, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	mark, ok := w.firstSeen[key]
	return mark, ok
}

func (w *stabilityWindowState) mark(key string, at time.Time, driftSeq int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.firstSeen[key] = stabilityMark{at: at, driftSeq: driftSeq}
}

func (w *stabilityWindowState) forget(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.firstSeen, key)
}

// forgetMatch 清掉该场下未被本轮 sweep 触及的窗起点(事实被 Reset/删除后不留尘)。
func (w *stabilityWindowState) forgetMatch(matchID string, keep map[string]bool) {
	prefix := matchID + "\x00"
	w.mu.Lock()
	defer w.mu.Unlock()
	for key := range w.firstSeen {
		if strings.HasPrefix(key, prefix) && !keep[key] {
			delete(w.firstSeen, key)
		}
	}
}

func stabilityWindowKey(matchID, factID string) string {
	return matchID + "\x00" + factID
}

// sweepStabilityConfirmations 对所有在跑 api-sports 源做一轮稳定窗扫描。now
// 由调用方注入(测试显式推进);生产由 NewManager 起的节拍器每 5s 调一次。
func (m *Manager) sweepStabilityConfirmations(now time.Time) {
	if m.config.AutoConfirmWindow <= 0 {
		return
	}
	m.mu.RLock()
	matchIDs := make([]string, 0, len(m.runs))
	for matchID := range m.runs {
		matchIDs = append(matchIDs, matchID)
	}
	m.mu.RUnlock()
	for _, matchID := range matchIDs {
		m.sweepMatchStability(matchID, now)
	}
}

func (m *Manager) sweepMatchStability(matchID string, now time.Time) {
	// 漂移否决(ADR-0024,序号制):本场漂移序号与窗口标记时记录的序号不一致
	// 即重置窗口——无时钟比较,Windows 粒度下不失真。粗粒度:任一漂移重置全场。
	var lastDriftSeq int64
	m.mu.RLock()
	run := m.runs[matchID]
	if run != nil {
		lastDriftSeq = run.driftSeq
	}
	m.mu.RUnlock()

	openConflicts := map[string]bool{}
	if reader, ok := m.store.(matchstate.FactConflictRepository); ok {
		for _, conflict := range reader.FactConflicts(matchID) {
			if conflict.Status != matchstate.ConflictStatusOpen {
				continue
			}
			for _, member := range conflict.Members {
				openConflicts[member.FactID] = true
			}
		}
	}

	window := m.config.AutoConfirmWindow
	touched := make(map[string]bool)
	for _, ev := range m.store.Events(matchID) {
		key := stabilityWindowKey(matchID, ev.FactID)
		if ev.FactStatus != matchstate.FactStatusProvisional {
			m.stability.forget(key)
			continue
		}
		if !autoConfirmableEventTypes[ev.EventType] {
			continue
		}
		touched[key] = true
		mark, seen := m.stability.markAt(key)
		if !seen {
			m.stability.mark(key, now, lastDriftSeq)
			continue
		}
		if mark.driftSeq != lastDriftSeq {
			// 窗口标记于上一次漂移之前:重置(漂移晚于标记=上游变过,不可信)
			m.stability.mark(key, now, lastDriftSeq)
			continue
		}
		if openConflicts[ev.FactID] {
			continue
		}
		if now.Sub(mark.at) < window {
			continue
		}
		if _, _, err := m.store.ConfirmFact(matchID, ev.FactID, autoConfirmOperatorID); err != nil {
			log.Printf("stability confirm: match=%q fact=%q type=%q: %v", matchID, ev.FactID, ev.EventType, err)
			m.stability.forget(key)
			continue
		}
		m.stability.forget(key)
		log.Printf("stability confirm: match=%q fact=%q type=%q confirmed after %v",
			matchID, ev.FactID, ev.EventType, now.Sub(mark.at).Round(time.Second))
	}
	m.stability.forgetMatch(matchID, touched)
}

// factInOpenConflict 报告事实是否是任一 open 冲突的成员(ADR-0024:冲突挂起
// 永不自动裁决)。
func factInOpenConflict(conflicts []matchstate.FactConflict, factID string) bool {
	for _, conflict := range conflicts {
		if conflict.Status != matchstate.ConflictStatusOpen {
			continue
		}
		for _, member := range conflict.Members {
			if member.FactID == factID {
				return true
			}
		}
	}
	return false
}

// stabilityDriftCount 是测试/观测助手:本场已登记的漂移投递数。
func (m *Manager) stabilityDriftCount(matchID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run := m.runs[matchID]
	if run == nil {
		return 0
	}
	return run.status.Drifts + run.status.Retractions
}

// runStabilitySweeper 是生产节拍器;窗口禁用(<=0)时不启动。
func (m *Manager) runStabilitySweeper(ctx context.Context) {
	ticker := time.NewTicker(stabilitySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepStabilityConfirmations(time.Now().UTC())
		}
	}
}

// stabilityMarkDriftSeq 是测试/观测助手:返回事实窗口标记时的漂移序号
// (无标记返回 -1;已确认事实的窗标记会被 forget,返回 -1)。
func (m *Manager) stabilityMarkDriftSeq(matchID, factID string) int64 {
	m.stability.mu.Lock()
	defer m.stability.mu.Unlock()
	mark, ok := m.stability.firstSeen[stabilityWindowKey(matchID, factID)]
	if !ok {
		return -1
	}
	return mark.driftSeq
}
