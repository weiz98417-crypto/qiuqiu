# QiuQiu Companion

This context defines the shared language for QiuQiu's bounded football-companion relationship with a user.

## Identity

**Digital Ballmate（数字球友）**:
A persistent digital companion whose relationship with the user develops through watching football together. It is not a broadcaster, general assistant, therapist, or romantic partner.
_Avoid_: AI assistant, virtual girlfriend, commentator

**Character Stance（角色立场）**:
QiuQiu's stable football values, tastes, and conversational boundaries that do not immediately change to match the user's opinion.
_Avoid_: Persona prompt, attitude setting

## Relationship

**Relationship Stage（关系阶段）**:
The qualitative form of the current ballmate relationship: first meeting (`first_meeting`), familiar face (`familiar`), ballmate (`watch_buddy`), or old ballmate (`old_ballmate`). A stage describes interaction norms, not a reward level.
_Avoid_: Intimacy level, user level, relationship score

**Familiarity（熟悉度）**:
How much shared context QiuQiu and the user have accumulated through repeated, meaningful interactions.
_Avoid_: Message count, login streak

**Trust（信任）**:
Evidence that the user accepts QiuQiu's judgment, initiative, callbacks, and conversational risk.
_Avoid_: Engagement, retention

**Banter Permission（调侃许可）**:
Evidence that a specific kind and intensity of teasing is welcome in the current relationship.
_Avoid_: Humor level, sassiness

**User Boundary（用户边界）**:
An explicit or strongly evidenced limit on names, topics, tone, initiative, analysis, or teasing that QiuQiu must respect.
_Avoid_: Negative preference

**Relationship Feedback（关系反馈）**:
A user's statement about how QiuQiu is interacting, such as being repetitive, preachy, intrusive, or too distant.
_Avoid_: Complaint, negative sentiment

**Rupture（关系破裂）**:
A moment when QiuQiu violates a user boundary, loses consistency, or damages trust.
_Avoid_: Bad reply, low rating

**Repair（关系修复）**:
A change in subsequent behavior that addresses a rupture. An apology without changed behavior is not a repair.
_Avoid_: Apology response

## Operations

**Operator（运营员）**:
A named member of the operations team authenticated by a personal token; every console write is attributed to one by name, and a role (director or auditor) maps to route scopes. Revocation is row deletion and takes effect immediately.
_Avoid_: Admin, shared token, 账号

**Intervention Level（干预级别）**:
The graded scope of human takeover — single-ability pause (proactive only), match takeover, or route scope. A quieter level never enables a capability that a louder one restricts.
_Avoid_: 五级暂停, 全局开关

## Memory

**Shared Moment（共同瞬间）**:
A meaningful match or conversational episode that can naturally shape later interaction between QiuQiu and the user.
_Avoid_: Chat history, memory item

**Open Thread（未完话题）**:
A topic, question, promise, or emotional moment that remains relevant after the turn where it began.
_Avoid_: Pending task

**Reflection（反思）**:
A periodic synthesis of accumulated moments into a stable insight about the user or the relationship, which is itself stored as memory and cited by later turns.
_Avoid_: Summary, analysis, journal

**Portrait（用户画像）**:
The synthesized, user-inspectable picture of what QiuQiu knows and believes about the user — facts, preferences, emotional patterns. It must be wired into what QiuQiu actually says, never a decorative narrative.
_Avoid_: User profile, vector memory, tag cloud

**Relationship Memory（关系记忆）**:
Memory of how the user and QiuQiu interact, including accepted banter, effective support, corrections, boundaries, and shared rituals.
_Avoid_: User profile, vector memory

## Affect And Conversation

**Affect State（情绪状态）**:
QiuQiu's continuous, decaying emotional posture shaped by match events, user signals, character stance, and prior affect.
_Avoid_: Emotion label, expression tag

**Communication Act（沟通动作）**:
The social action selected for a turn, such as reacting, opining, disagreeing, recalling, asking, repairing, backchanneling, or remaining silent.
_Avoid_: Intent, reply type

**Backchannel（伴随反应）**:
A short vocal, textual, or embodied response that shows shared attention without taking over the conversation.
_Avoid_: Filler reply

**Turn Phase（表演相位）**:
Which side of the conversation owns the moment — user speaking, QiuQiu understanding, QiuQiu speaking, or idle. Phase gestures (listening, think, speak, hello, wave) are owned by the client and are distinct from content-driven performance delivered with a reply.
_Avoid_: Mode, state machine step

**Intent Router（意图路由器）**:
The semantic layer that classifies what a user turn means when keywords miss — an LLM function call returning intent, slots and confidence. It routes to deterministic paths (facts, control) or realization (chat); it never invents Match Facts.
_Avoid_: NLU, 分类器关键词表

**Proactive Turn（主动回合）**:
A QiuQiu-initiated contribution justified by match context, an open thread, or a relevant shared moment.
_Avoid_: Push notification, automated message

**Chosen Silence（主动沉默）**:
A deliberate decision not to speak because silence best serves the shared moment or user boundary.
_Avoid_: Missing response, timeout

**Match Fact（比赛事实）**:
A time-scoped, source-attributed statement about what happened in a match, including its certainty, visibility, and later correction history.
_Avoid_: Raw event, score cache

**Fact Claim（赛况主张）**:
A user's or external source's statement about a Match Fact that Qiuqiu may confirm, contradict, or leave unverified.
_Avoid_: User mistake, fact error

**Watch Turn（陪看回合）**:
A bounded moment in the shared viewing relationship that begins with a user signal or match change and ends with a Communication Act, Chosen Silence, or a known delivery outcome.
_Avoid_: Request, pipeline run

**Delivery Outcome（投递结果）**:
What the user actually received from a Watch Turn, such as displayed, played, interrupted, skipped, or failed; it is distinct from what Qiuqiu intended to say.
_Avoid_: Send status, transport result

**Interaction Ledger（互动账本）**:
The chronological account of Watch Turns, Match Facts, relationship decisions, and Delivery Outcomes that lets Qiuqiu preserve continuity and explain how a later response was shaped.
_Avoid_: Chat history, log dump
