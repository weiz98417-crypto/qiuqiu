package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"strings"
	"time"

	"qiuqiu/internal/structured"
)

// 画像冲突操作集（openspec/changes/portrait-maintenance 阶段一）：Reflection
// 把一条新主张并入权威层前，先判定它与同 topic 现存有效条目的关系，再按
// 判定确定性落地。LLM 只判关系（走 structured seam，第三家生产消费者），
// 写库全部是纯 SQL/Go——「schema 与结果类型漂移」与「判完乱写」两类 bug
// 从结构上不存在。墓碑纪律：任何分支都不物理删，失效=valid_to 封口。

// PortraitOp 是冲突操作集的四种落地动作。
type PortraitOp string

const (
	PortraitOpAdd    PortraitOp = "ADD"    // 新信息且无冲突：开新条目（槽内不得有开放行）
	PortraitOpUpdate PortraitOp = "UPDATE" // 同槽新值取代旧值：旧条目封口 + 新条目生效
	PortraitOpDelete PortraitOp = "DELETE" // 主张明示旧条目作废：旧条目封口 + 开放墓碑遮蔽同槽
	PortraitOpNoop   PortraitOp = "NOOP"   // 重复或无新信息：不写
)

// OpDecision mirrors the decide_portrait_op function-call arguments. jsonschema
// tag 由 structured.Extract 反射成工具参数 schema——invopop 的枚举语法是重复
// 的 `enum=值` 指令（router.Result 同款）。TargetID 指现存有效条目的行 id
// （migration 051 的 BIGSERIAL 主键），ADD/NOOP 必须为 0。
type OpDecision struct {
	Op       PortraitOp `json:"op" jsonschema:"description=对现存条目的操作，必须从这个枚举里选,required,enum=ADD,enum=UPDATE,enum=DELETE,enum=NOOP"`
	TargetID int64      `json:"targetId,omitempty" jsonschema_description:"UPDATE/DELETE 指向的现存条目 id；ADD/NOOP 填 0"`
	Reason   string     `json:"reason" jsonschema:"description=一句话判定依据，入审计"`
}

// PortraitClaim is one new claim waiting to merge into the authoritative
// overlay layer (a Reflection beat consolidates what the user stated).
type PortraitClaim struct {
	Topic    string
	SubTopic string
	Content  string
}

// normalize 去掉首尾空白；空主张不进判定。
func (c PortraitClaim) normalize() PortraitClaim {
	return PortraitClaim{
		Topic:    strings.TrimSpace(c.Topic),
		SubTopic: strings.TrimSpace(c.SubTopic),
		Content:  strings.TrimSpace(c.Content),
	}
}

// PortraitOpDecider judges one new claim against the valid entries of the
// same topic. 生产实现走 structured.Extract；测试/evals 用脚本桩（pr tier
// 脚本 LLM 先例=scriptedRouter）。
type PortraitOpDecider interface {
	DecidePortraitOp(ctx context.Context, claim PortraitClaim, existing []PortraitOverlay) (OpDecision, error)
}

// PortraitMaintainer is the deterministic half of the op set: privacy gate,
// decider, validated landing, audit. 判定器缺席=盲 ADD（现状行为）——删掉操
// 作集，Reflection 回到只累积不修证，这是 success criteria 的删除测试。
type PortraitMaintainer struct {
	store   PortraitOverlayStore
	decider PortraitOpDecider
	audit   AuditSink
}

// NewPortraitMaintainer wires the maintainer; decider/audit may be nil
// (盲 ADD / 只落日志).
func NewPortraitMaintainer(store PortraitOverlayStore, decider PortraitOpDecider, audit AuditSink) *PortraitMaintainer {
	return &PortraitMaintainer{store: store, decider: decider, audit: audit}
}

// 审计 reason codes：操作集的每个判定都进 memory_extraction_audit（ADR-0006
// fact-first culture 在画像侧的延伸）。
const (
	ReasonPortraitOpApplied = "portrait_op_applied"
	ReasonPortraitOpNoop    = "portrait_op_noop"
	ReasonPortraitOpSkipped = "portrait_op_skipped"
)

// Consolidate merges one claim into the authoritative layer:
// privacy 前置 → 取同 topic 现存有效条目 → 判定（无判定器=盲 ADD）→ 校验
// TargetID → 确定性落地 → 审计。判定或落地失败审计后原样上抛，绝不半写
// （UPDATE 的封口+新行在 store 侧单事务内）。
func (m *PortraitMaintainer) Consolidate(ctx context.Context, userID string, claim PortraitClaim) (OpDecision, error) {
	if m == nil || m.store == nil {
		return OpDecision{}, ErrUnavailable
	}
	claim = claim.normalize()
	if claim.Topic == "" || claim.SubTopic == "" || claim.Content == "" {
		return OpDecision{}, fmt.Errorf("portrait claim needs topic, subTopic and content")
	}
	// Privacy 前置纪律（migration 041）：任何写入前先过隐私生命周期，
	// 删除用户（已完成或进行中）的画像一律拒写。
	if err := m.store.Check(ctx, userID); err != nil {
		return OpDecision{}, err
	}
	decision := OpDecision{Op: PortraitOpAdd, Reason: "无判定器，按现状盲 ADD"}
	if m.decider != nil {
		existing, err := m.validTopicEntries(ctx, userID, claim.Topic)
		if err != nil {
			return OpDecision{}, err
		}
		decision, err = m.decider.DecidePortraitOp(ctx, claim, existing)
		if err != nil {
			m.recordAudit(ctx, userID, claim, OpDecision{Op: PortraitOpNoop, Reason: err.Error()}, ReasonPortraitOpSkipped)
			return OpDecision{}, fmt.Errorf("portrait op decision: %w", err)
		}
		if err := validateDecision(decision, existing); err != nil {
			m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpSkipped)
			return decision, err
		}
	} else if row, ok := openSlotRow(m, ctx, userID, claim); ok {
		// 盲 ADD 撞同槽开放行时降级 UPDATE（保持历史 upsert 行为）：
		// 无 key 环境（pr tier/本地）没有判定器，第二条主张不能静默丢弃；
		// 判定器在场的误判 ADD 仍由 store 的同槽唯一约束拒收。
		decision = OpDecision{Op: PortraitOpUpdate, TargetID: row, Reason: "无判定器，同槽已有开放行，按 UPDATE 收口"}
	}
	if _, err := m.store.ApplyPortraitOp(ctx, userID, decision, claim); err != nil {
		m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpSkipped)
		return decision, err
	}
	if decision.Op == PortraitOpNoop {
		m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpNoop)
		return decision, nil
	}
	m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpApplied)
	return decision, nil
}

// openSlotRow 在无判定器的盲 ADD 前探测同槽开放行；命中则返回其行 id，
// 由调用方降级为 UPDATE（收口旧行、写入新行），第二条主张不再被丢弃。
func openSlotRow(m *PortraitMaintainer, ctx context.Context, userID string, claim PortraitClaim) (int64, bool) {
	current, err := m.store.CurrentPortrait(ctx, userID)
	if err != nil {
		return 0, false
	}
	for _, overlay := range current {
		if !overlay.Deleted && overlay.Topic == claim.Topic && overlay.SubTopic == claim.SubTopic {
			return overlay.ID, true
		}
	}
	return 0, false
}

// validTopicEntries 取同 topic 的现存有效条目（非墓碑、窗内）作为判定输入。
func (m *PortraitMaintainer) validTopicEntries(ctx context.Context, userID, topic string) ([]PortraitOverlay, error) {
	current, err := m.store.CurrentPortrait(ctx, userID)
	if err != nil {
		return nil, err
	}
	existing := make([]PortraitOverlay, 0, len(current))
	for _, overlay := range current {
		if overlay.Deleted || overlay.Topic != topic {
			continue
		}
		existing = append(existing, overlay)
	}
	return existing, nil
}

// validateDecision locks the decider contract: UPDATE/DELETE must point at a
// listed valid entry, ADD/NOOP must not point anywhere. 幻觉 TargetID 在这里
// 拦下，不落库。
func validateDecision(decision OpDecision, existing []PortraitOverlay) error {
	switch decision.Op {
	case PortraitOpAdd, PortraitOpNoop:
		if decision.TargetID != 0 {
			return fmt.Errorf("%s must not carry a targetId, got %d", decision.Op, decision.TargetID)
		}
		return nil
	case PortraitOpUpdate, PortraitOpDelete:
		for _, overlay := range existing {
			if overlay.ID == decision.TargetID {
				return nil
			}
		}
		return fmt.Errorf("%s target %d is not a valid entry", decision.Op, decision.TargetID)
	default:
		return fmt.Errorf("unknown op %q", decision.Op)
	}
}

// recordAudit writes one op decision into the extraction audit ledger; failures
// degrade to logs, never block the beat (same discipline as recordAudit).
func (m *PortraitMaintainer) recordAudit(ctx context.Context, userID string, claim PortraitClaim, decision OpDecision, reasonCode string) {
	if m.audit == nil {
		return
	}
	entry := ExtractionAudit{
		MomentID:   portraitClaimAuditID(claim),
		UserID:     userID,
		Kind:       MomentUserFact,
		ReasonCode: reasonCode,
		Detail:     fmt.Sprintf("op=%s target=%d topic=%s/%s reason=%s", decision.Op, decision.TargetID, claim.Topic, claim.SubTopic, decision.Reason),
		CreatedAt:  time.Now().UTC(),
	}
	auditCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := m.audit.RecordExtraction(auditCtx, entry); err != nil {
		log.Printf("memory: record portrait op audit: %v", err)
	}
}

func portraitClaimAuditID(claim PortraitClaim) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(claim.Topic + "\x00" + claim.SubTopic + "\x00" + claim.Content))
	return fmt.Sprintf("portrait:%s/%s:%016x", claim.Topic, claim.SubTopic, hash.Sum64())
}

// portraitOpToolName is the single function the judge model must call.
const portraitOpToolName = "decide_portrait_op"

// LLMPortraitOps decides through the structured seam, exactly like
// directordraft (first consumer) and the router (second).
type LLMPortraitOps struct {
	client *structured.Client
}

// NewLLMPortraitOpDecider wires the judge; nil client degrades every decision
// to an error so the maintainer skips instead of guessing.
func NewLLMPortraitOpDecider(client *structured.Client) *LLMPortraitOps {
	return &LLMPortraitOps{client: client}
}

const portraitOpSystemPrompt = `你是陪看足球助手"球球"的画像维护判定器。给你一条用户新主张和同一画像主题下现存的长期条目（含 id），你只判定新主张该执行哪种操作，不回答、不闲聊、不虚构条目。
操作定义：
- ADD：新信息且与现存条目不冲突（新槽位、新话题）。
- UPDATE：与某条目同槽位但内容已被用户更新（换主队、改口味），旧条目作废、新值取代。
- DELETE：用户明示某现存条目不再成立或要求忘掉它，且没有新值可以取代。
- NOOP：新主张与现存条目重复，或不含可长期保存的新信息。
约束：UPDATE/DELETE 的 targetId 必须从列出的条目 id 里选；ADD/NOOP 的 targetId 填 0。reason 用一句话中文说明判定依据。`

// DecidePortraitOp forces the single-tool extraction and validates the decoded
// decision against the listed entries. Exactly one attempt: any transport or
// contract failure returns an error and the maintainer skips the write
// (ADR-0012 韧性策略：传输零重试，调用方自行决定策略).
func (d *LLMPortraitOps) DecidePortraitOp(ctx context.Context, claim PortraitClaim, existing []PortraitOverlay) (OpDecision, error) {
	if d == nil || d.client == nil {
		return OpDecision{}, ErrNotSupported
	}
	var entries strings.Builder
	for _, overlay := range existing {
		fmt.Fprintf(&entries, "- id=%d slot=%s：%s\n", overlay.ID, overlay.SubTopic, overlay.Content)
	}
	userContent := fmt.Sprintf("画像主题：%s\n槽位：%s\n现存条目：\n%s新主张：%s",
		claim.Topic, claim.SubTopic, entries.String(), claim.Content)
	decision, err := structured.Extract[OpDecision](ctx, d.client, structured.CallOptions{
		SystemPrompt:    portraitOpSystemPrompt,
		UserContent:     userContent,
		ToolName:        portraitOpToolName,
		ToolDescription: "判定用户新主张对画像现存条目的冲突操作",
		Temperature:     0.1,
		MaxTokens:       160,
	})
	if err != nil {
		return OpDecision{}, err
	}
	decision.Op = PortraitOp(strings.TrimSpace(string(decision.Op)))
	decision.Reason = strings.TrimSpace(decision.Reason)
	switch decision.Op {
	case PortraitOpAdd, PortraitOpUpdate, PortraitOpDelete, PortraitOpNoop:
	default:
		return OpDecision{}, fmt.Errorf("unknown op %q", decision.Op)
	}
	return decision, nil
}

var _ PortraitOpDecider = (*LLMPortraitOps)(nil)
