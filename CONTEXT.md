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

## Memory

**Shared Moment（共同瞬间）**:
A meaningful match or conversational episode that can naturally shape later interaction between QiuQiu and the user.
_Avoid_: Chat history, memory item

**Open Thread（未完话题）**:
A topic, question, promise, or emotional moment that remains relevant after the turn where it began.
_Avoid_: Pending task

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

**Proactive Turn（主动回合）**:
A QiuQiu-initiated contribution justified by match context, an open thread, or a relevant shared moment.
_Avoid_: Push notification, automated message

**Chosen Silence（主动沉默）**:
A deliberate decision not to speak because silence best serves the shared moment or user boundary.
_Avoid_: Missing response, timeout
