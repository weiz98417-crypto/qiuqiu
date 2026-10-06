package memory

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"regexp"
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
// （migration 051 的 BIGSERIAL 主键），ADD/NOOP 必须为 0。shadowSlots 是
// 阶段二的合成槽遮蔽：判定 UPDATE/DELETE 时一并指出该主张涉及的 Memobase
// 合成槽（如换主队 → basic_info/favorite_team），落地=对槽写开放墓碑。
type OpDecision struct {
	Op       PortraitOp `json:"op" jsonschema:"description=对现存条目的操作，必须从这个枚举里选,required,enum=ADD,enum=UPDATE,enum=DELETE,enum=NOOP"`
	TargetID int64      `json:"targetId,omitempty" jsonschema_description:"UPDATE/DELETE 指向的现存条目 id；ADD/NOOP 填 0"`
	Reason   string     `json:"reason" jsonschema:"description=一句话判定依据，入审计"`
	// ShadowSlots 只在 UPDATE/DELETE 有意义：新值/作废同时使该槽的 Memobase
	// 合成条目过期（权威层换值后合成层残留旧值会继续织入措辞）。槽位必须
	// 从提示里列出的候选清单中选，可以不填。
	ShadowSlots []PortraitSlotRef `json:"shadowSlots,omitempty" jsonschema_description:"本主张同时使其过期的 Memobase 合成槽（换主队/球员偏好时填对应槽），从候选清单里选；没有则省略"`
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
	// reviewer 是写回 dry-run 审查（memory-scoring 8.3）：nil = 写回直落
	// （现状行为，success criteria 的缺席测试）。
	reviewer PortraitWritebackReviewer
}

// NewPortraitMaintainer wires the maintainer; decider/audit/reviewer may be
// nil (盲 ADD / 只落日志 / 写回直落=现状).
func NewPortraitMaintainer(store PortraitOverlayStore, decider PortraitOpDecider, audit AuditSink, reviewer PortraitWritebackReviewer) *PortraitMaintainer {
	return &PortraitMaintainer{store: store, decider: decider, audit: audit, reviewer: reviewer}
}

// 审计 reason codes：操作集的每个判定都进 memory_extraction_audit（ADR-0006
// fact-first culture 在画像侧的延伸）。
const (
	ReasonPortraitOpApplied     = "portrait_op_applied"
	ReasonPortraitOpNoop        = "portrait_op_noop"
	ReasonPortraitOpSkipped     = "portrait_op_skipped"
	ReasonPortraitShadowApplied = "portrait_shadow_applied"
	ReasonPortraitShadowSkipped = "portrait_shadow_skipped"
)

// Consolidate merges one claim into the authoritative layer:
// privacy 前置 → 写回 dry-run 审查（8.3，reviewer 缺席=直落）→ 取同 topic
// 现存有效条目 → 判定（无判定器=盲 ADD）→ 校验 TargetID → 确定性落地 →
// 审计。判定或落地失败审计后原样上抛，绝不半写（UPDATE 的封口+新行在
// store 侧单事务内）。
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
	// 确定性红线硬门（memory-scoring 8.3 审查修正）：比分形态是「比赛事
	// 实进画像」最典型的越权（ADR-0006：合成/画像产物永不断言 Match
	// Facts）——零 LLM 成本先拦，不依赖 reviewer 在场。三段式数字（3-4-3
	// 阵型）豁免。
	if reason := claimScoreRedline(claim.Content); reason != "" {
		decision := OpDecision{Op: PortraitOpNoop, Reason: "writeback_rejected: " + reason}
		m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpNoop)
		return decision, nil
	}
	// 写回 dry-run（memory-scoring 8.3）：一次轻量 LLM 自查（与画像矛盾/
	// 噪声闲聊/越权记比赛事实），不过审记审计跳过——一次额外调用换画像
	// 质量（Letta sleep-time compute / MIRIX Auto-Dream 同模式）。评审缺
	// 席=直落（现状，缺席测试）；审查失败也直落（reviewer 是质量闸不是
	// 可用性闸，故障不阻断画像维护；比分红线已由上方硬门确定性兜住）。
	if m.reviewer != nil {
		existing, entriesErr := m.validTopicEntries(ctx, userID, claim.Topic)
		if entriesErr == nil {
			reviewCtx, reviewCancel := context.WithTimeout(ctx, 10*time.Second)
			verdict, reviewErr := m.reviewer.ReviewWriteback(reviewCtx, userID, claim, existing)
			reviewCancel()
			if reviewErr == nil && !verdict.OK {
				decision := OpDecision{Op: PortraitOpNoop, Reason: "writeback_rejected: " + verdict.Reason}
				m.recordAudit(ctx, userID, claim, decision, ReasonPortraitOpNoop)
				return decision, nil
			}
		}
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
	m.landShadowSlots(ctx, userID, claim, decision)
	return decision, nil
}

// landShadowSlots 落地合成槽遮蔽（阶段二收口）：UPDATE/DELETE 使权威层换值
// 的同时，对判定器指认的 Memobase 合成槽写开放墓碑——ResolvePortrait 每次
// 读取都遮蔽该槽，不怕 Memobase 从旧 blob 重提取。失败记审计后继续（遮蔽
// 是尽力而为的收口，绝不回滚已落地的权威层写入）；不遮蔽主张自己的槽。
func (m *PortraitMaintainer) landShadowSlots(ctx context.Context, userID string, claim PortraitClaim, decision OpDecision) {
	if decision.Op != PortraitOpUpdate && decision.Op != PortraitOpDelete {
		return
	}
	for _, slot := range decision.ShadowSlots {
		slot.Topic = strings.TrimSpace(slot.Topic)
		slot.SubTopic = strings.TrimSpace(slot.SubTopic)
		if slot.Topic == "" || slot.SubTopic == "" || (slot.Topic == claim.Topic && slot.SubTopic == claim.SubTopic) {
			continue
		}
		shadowCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := m.store.Delete(shadowCtx, userID, slot.Topic, slot.SubTopic)
		cancel()
		if err != nil {
			m.recordAudit(ctx, userID, claim, OpDecision{Op: PortraitOpNoop, Reason: err.Error()}, ReasonPortraitShadowSkipped)
			log.Printf("memory: portrait shadow slot %s/%s for user %q: %v", slot.Topic, slot.SubTopic, userID, err)
			continue
		}
		m.recordAudit(ctx, userID, PortraitClaim{Topic: slot.Topic, SubTopic: slot.SubTopic}, decision, ReasonPortraitShadowApplied)
	}
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
// directordraft (first consumer) and the router (second). shadowSlots 是可
// 遮蔽合成槽候选（config 下发），进提示与 user content，供判定器在
// UPDATE/DELETE 时指认——schema 本身由 OpDecision 反射，候选不进 schema。
type LLMPortraitOps struct {
	client      *structured.Client
	shadowSlots []PortraitSlotRef
}

// NewLLMPortraitOpDecider wires the judge; nil client degrades every decision
// to an error so the maintainer skips instead of guessing. shadowSlots 可空。
func NewLLMPortraitOpDecider(client *structured.Client, shadowSlots []PortraitSlotRef) *LLMPortraitOps {
	return &LLMPortraitOps{client: client, shadowSlots: shadowSlots}
}

const portraitOpSystemPrompt = `你是陪看足球助手"球球"的画像维护判定器。给你一条用户新主张和同一画像主题下现存的长期条目（含 id），你只判定新主张该执行哪种操作，不回答、不闲聊、不虚构条目。
操作定义：
- ADD：新信息且与现存条目不冲突（新槽位、新话题）。
- UPDATE：与某条目同槽位但内容已被用户更新（换主队、改口味），旧条目作废、新值取代。
- DELETE：用户明示某现存条目不再成立或要求忘掉它，且没有新值可以取代。
- NOOP：新主张与现存条目重复，或不含可长期保存的新信息。
约束：UPDATE/DELETE 的 targetId 必须从列出的条目 id 里选；ADD/NOOP 的 targetId 填 0。reason 用一句话中文说明判定依据。
shadowSlots：仅 UPDATE/DELETE 时可填——主张换掉了某个长期偏好（主队、球员偏好等），而提示里列出的 Memobase 合成槽位候选中恰有对应槽，就把它填进 shadowSlots（数组，每项 {topic, subTopic}）；没有对应槽或拿不准就省略。`

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
	shadowHint := ""
	if len(d.shadowSlots) > 0 {
		var slots strings.Builder
		for _, slot := range d.shadowSlots {
			fmt.Fprintf(&slots, "%s/%s、", slot.Topic, slot.SubTopic)
		}
		shadowHint = fmt.Sprintf("可遮蔽的 Memobase 合成槽位候选：%s（本主张使其过期时，在 shadowSlots 里填对应槽）", strings.TrimSuffix(slots.String(), "、"))
	}
	userContent := fmt.Sprintf("画像主题：%s\n槽位：%s\n现存条目：\n%s新主张：%s\n%s",
		claim.Topic, claim.SubTopic, entries.String(), claim.Content, shadowHint)
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
	// shadowSlots 只在 UPDATE/DELETE 语义成立：ADD/NOOP 一律清空（模型偶发
	// 乱填时保守丢弃），UPDATE/DELETE 的空槽引用逐条滤掉。
	if decision.Op != PortraitOpUpdate && decision.Op != PortraitOpDelete {
		decision.ShadowSlots = nil
	} else {
		shadows := make([]PortraitSlotRef, 0, len(decision.ShadowSlots))
		for _, slot := range decision.ShadowSlots {
			slot.Topic = strings.TrimSpace(slot.Topic)
			slot.SubTopic = strings.TrimSpace(slot.SubTopic)
			if slot.Topic != "" && slot.SubTopic != "" {
				shadows = append(shadows, slot)
			}
		}
		decision.ShadowSlots = shadows
	}
	return decision, nil
}

var _ PortraitOpDecider = (*LLMPortraitOps)(nil)

// PortraitWritebackReviewer 是写回 dry-run 审查（memory-scoring 8.3）：
// claim 落权威层前的一次自查——与画像矛盾、噪声闲聊碎片、越权记比赛
// 事实（宪法红线：比赛事实永不进画像）。ok=false 时 claim 记审计跳过。
// 评审是质量闸不是可用性闸：缺席或故障都直落（现状行为）。
type PortraitWritebackReviewer interface {
	ReviewWriteback(ctx context.Context, userID string, claim PortraitClaim, existing []PortraitOverlay) (WritebackVerdict, error)
}

// WritebackVerdict 是审查结论：ok=false 时 reason 必须给出中文依据（进
// 审计链可查）。
type WritebackVerdict struct {
	OK     bool   `json:"ok" jsonschema_description:"true=可落库，false=拒绝"`
	Reason string `json:"reason" jsonschema_description:"一句话中文依据；拒绝时说明命中哪类问题"`
}

const writebackReviewToolName = "review_portrait_writeback"

const writebackReviewSystemPrompt = `你是陪看足球助手"球球"的画像写回审查器。给你一条即将写入用户长期画像的主张和同一画像主题下的现存条目，你只输出审查结论，不回答、不闲聊。
审查三类问题（任一命中即 ok=false）：
1. 矛盾：主张与现存条目直接打架（且不是「用户改主意」式的更新——更新由判定器另行处理，你只拦真矛盾）。
2. 噪声：闲聊碎片、一次性玩笑、不含可长期保存的信息。
3. 越权红线：主张是比赛事实（比分、进球、红牌、VAR 判罚等赛况陈述）——比赛事实永不进用户画像，这是宪法级红线。
均不命中（正常的长期偏好、口味、习惯、个人信息）则 ok=true，reason 用一句话中文说明。`

// LLMWritebackReviewer is the production reviewer via the structured seam
// (与 LLMPortraitOps 同形：LLM 只判结论，写库全在确定性层)。
type LLMWritebackReviewer struct {
	client *structured.Client
}

func NewLLMWritebackReviewer(client *structured.Client) *LLMWritebackReviewer {
	return &LLMWritebackReviewer{client: client}
}

// ReviewWriteback forces the single-tool extraction; transport/contract 失败
// 原样上抛——调用方（maintainer）对失败直落，审查故障不阻断画像维护。
func (r *LLMWritebackReviewer) ReviewWriteback(ctx context.Context, userID string, claim PortraitClaim, existing []PortraitOverlay) (WritebackVerdict, error) {
	if r == nil || r.client == nil {
		return WritebackVerdict{}, ErrNotSupported
	}
	var entries strings.Builder
	for _, overlay := range existing {
		fmt.Fprintf(&entries, "- %s：%s\n", overlay.SubTopic, overlay.Content)
	}
	userContent := fmt.Sprintf("画像主题：%s\n槽位：%s\n现存条目：\n%s新主张：%s",
		claim.Topic, claim.SubTopic, entries.String(), claim.Content)
	return structured.Extract[WritebackVerdict](ctx, r.client, structured.CallOptions{
		SystemPrompt:    writebackReviewSystemPrompt,
		UserContent:     userContent,
		ToolName:        writebackReviewToolName,
		ToolDescription: "审查一条画像写回主张能否落库",
		Temperature:     0.1,
		MaxTokens:       120,
	})
}

var _ PortraitWritebackReviewer = (*LLMWritebackReviewer)(nil)

// matchScorePattern 是比分形态（1-0 / 2 比 1；冒号形态与时间戳（02:30）
// 局部不可分，留 LLM reviewer 提示层管——硬门只收低误杀形态。两位数内——足球比分
// 不会上双位数比双位数的常见形态之外仍按词边界约束）。
var matchScorePattern = regexp.MustCompile(`\d{1,2}\s*[-:：比]\s*\d{1,2}`)

// claimScoreRedline 是写回的确定性红线硬门：比分形态的主张零 LLM 成本
// 先拦（ADR-0006：画像产物永不断言 Match Facts）。三段式数字（3-4-3 阵
// 型、02:30 时间戳类）豁免——match 后紧随又一段分隔数字的不算比分。
// 返回空串 = 未命中红线。
func claimScoreRedline(content string) string {
	for _, loc := range matchScorePattern.FindAllStringIndex(content, -1) {
		rest := content[loc[1]:]
		if strings.HasPrefix(rest, "-") || strings.HasPrefix(rest, "－") || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "：") {
			continue
		}
		return "比分属比赛事实，永不进画像"
	}
	return ""
}
