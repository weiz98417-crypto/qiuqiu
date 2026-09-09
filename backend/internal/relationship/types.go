package relationship

import (
	"encoding/json"
	"time"
)

type SignalKind string

const (
	SignalSessionOpened  SignalKind = "session_opened"
	SignalUserTurn       SignalKind = "user_turn"
	SignalMatchEvent     SignalKind = "match_event"
	SignalDeliveryResult SignalKind = "delivery_result"
)

type FactMode string

const (
	FactModeNone          FactMode = "none"
	FactModeAnchored      FactMode = "anchored"
	FactModeDeterministic FactMode = "deterministic"
	FactModeUnverified    FactMode = "unverified"
)

type Signal struct {
	ID           string          `json:"id"`
	TraceID      string          `json:"traceId,omitempty"`
	Kind         SignalKind      `json:"kind"`
	UserID       string          `json:"userId"`
	MatchID      string          `json:"matchId"`
	OccurredAt   time.Time       `json:"occurredAt"`
	ReceivedAt   time.Time       `json:"receivedAt"`
	FactRevision string          `json:"factRevision,omitempty"`
	User         *UserSignal     `json:"user,omitempty"`
	Match        *MatchSignal    `json:"match,omitempty"`
	Delivery     *DeliverySignal `json:"delivery,omitempty"`
	Grounding    GroundedContent `json:"grounding"`
}

type UserSignal struct {
	Text string    `json:"text"`
	Cues []UserCue `json:"cues,omitempty"`
}

type UserCueKind string

const (
	CueStablePreference      UserCueKind = "stable_preference"
	CueContinuedThread       UserCueKind = "continued_thread"
	CueOpenThreadReady       UserCueKind = "open_thread_ready"
	CueNeedsSilence          UserCueKind = "needs_silence"
	CueOpinionConflict       UserCueKind = "opinion_conflict"
	CueAcceptedJudgment      UserCueKind = "accepted_judgment"
	CueAcceptedInitiative    UserCueKind = "accepted_initiative"
	CueBanterAllowed         UserCueKind = "banter_allowed"
	CueBanterDenied          UserCueKind = "banter_denied"
	CueSharedMomentRecalled  UserCueKind = "shared_moment_recalled"
	CueContinuedDisagreement UserCueKind = "continued_disagreement"
)

type UserCue struct {
	Kind  UserCueKind `json:"kind"`
	Scope string      `json:"scope,omitempty"`
}

type MatchSignal struct {
	EventID               string `json:"eventId"`
	EventType             string `json:"eventType"`
	Intensity             int    `json:"intensity"`
	Confirmed             bool   `json:"confirmed"`
	OutputAllowed         bool   `json:"outputAllowed"`
	Critical              bool   `json:"critical"`
	UserSpeaking          bool   `json:"userSpeaking"`
	NormalCooldownSeconds int    `json:"normalCooldownSeconds,omitempty"`
	Description           string `json:"description,omitempty"`
	TeamName              string `json:"teamName,omitempty"`
	PlayerName            string `json:"playerName,omitempty"`
	RevisionOf            string `json:"revisionOf,omitempty"`
}

type DeliverySignal struct {
	DecisionID    string   `json:"decisionId"`
	State         string   `json:"state"`
	Purpose       string   `json:"purpose,omitempty"`
	UsedMemoryIDs []string `json:"usedMemoryIds,omitempty"`
}

type GroundedContent struct {
	Intent             string   `json:"intent"`
	ReliableText       string   `json:"reliableText,omitempty"`
	RequiredAnchors    []string `json:"requiredAnchors,omitempty"`
	FactMode           FactMode `json:"factMode"`
	SourceEventIDs     []string `json:"sourceEventIds,omitempty"`
	RecentPhraseHashes []uint64 `json:"recentPhraseHashes,omitempty"`
}

type RelationshipStage string

const (
	StageFirstMeeting RelationshipStage = "first_meeting"
	StageFamiliar     RelationshipStage = "familiar"
	StageWatchBuddy   RelationshipStage = "watch_buddy"
	StageOldBallmate  RelationshipStage = "old_ballmate"
)

type RelationshipState struct {
	SchemaVersion       int                     `json:"schemaVersion"`
	UserID              string                  `json:"userId"`
	FirstMetAt          *time.Time              `json:"firstMetAt,omitempty"`
	GreetingDeliveredAt *time.Time              `json:"greetingDeliveredAt,omitempty"`
	Stage               RelationshipStage       `json:"stage"`
	Evidence            StageEvidence           `json:"evidence"`
	Trust               TrustEvidence           `json:"trust"`
	Banter              map[string]Permission   `json:"banter,omitempty"`
	Boundaries          []UserBoundary          `json:"boundaries,omitempty"`
	Preferences         RelationshipPreferences `json:"preferences"`
	Repair              RepairState             `json:"repair"`
	Taste               TasteState              `json:"taste"`
	MeaningfulAt        time.Time               `json:"meaningfulAt,omitempty"`
	Version             int64                   `json:"version"`
	UpdatedAt           time.Time               `json:"updatedAt"`
}

type TrustEvidence struct {
	JudgmentAcceptedAt    *time.Time `json:"judgmentAcceptedAt,omitempty"`
	InitiativeAcceptedAt  *time.Time `json:"initiativeAcceptedAt,omitempty"`
	CallbackAcceptedAt    *time.Time `json:"callbackAcceptedAt,omitempty"`
	CorrectionContinuedAt *time.Time `json:"correctionContinuedAt,omitempty"`
}

type RelationshipPreferences struct {
	InitiativeMode     string   `json:"initiativeMode"`
	AnalysisAppetite   string   `json:"analysisAppetite"`
	PreferredName      string   `json:"preferredName,omitempty"`
	AllowedAddressing  []string `json:"allowedAddressing,omitempty"`
	ProfanityEnabled   bool     `json:"profanityEnabled"`
	ContinuousDialogue bool     `json:"continuousDialogue"`
}

type TasteState struct {
	StylePreferences []TastePreference `json:"stylePreferences,omitempty"`
	PlayerArchetypes []TastePreference `json:"playerArchetypes,omitempty"`
}

type TastePreference struct {
	Subject       string    `json:"subject"`
	Direction     string    `json:"direction"`
	Confidence    float64   `json:"confidence"`
	EvidenceRefs  []string  `json:"evidenceRefs,omitempty"`
	LastUpdatedAt time.Time `json:"lastUpdatedAt"`
}

type StageEvidence struct {
	SharedMatches          map[string]time.Time `json:"sharedMatches,omitempty"`
	StablePreferences      int                  `json:"stablePreferences"`
	ExplicitBoundaries     int                  `json:"explicitBoundaries"`
	ContinuedThreads       int                  `json:"continuedThreads"`
	AcceptedJudgments      int                  `json:"acceptedJudgments"`
	AcceptedInitiatives    int                  `json:"acceptedInitiatives"`
	AllowedBanterScopes    int                  `json:"allowedBanterScopes"`
	SharedMomentsRecalled  int                  `json:"sharedMomentsRecalled"`
	ContinuedDisagreements int                  `json:"continuedDisagreements"`
	CompletedRepairs       int                  `json:"completedRepairs"`
}

type Permission struct {
	Status         string `json:"status"`
	EvidenceCount  int    `json:"evidenceCount"`
	SourceSignalID string `json:"sourceSignalId"`
}

type UserBoundary struct {
	ID             string     `json:"id,omitempty"`
	Scope          string     `json:"scope"`
	Rule           string     `json:"rule"`
	Explicit       bool       `json:"explicit"`
	SourceSignalID string     `json:"sourceSignalId"`
	SourceTraceID  string     `json:"sourceTraceId,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
}

type RepairState struct {
	Active          bool      `json:"active"`
	Category        string    `json:"category,omitempty"`
	TriggerSignalID string    `json:"triggerSignalId,omitempty"`
	BehaviorChanges []string  `json:"behaviorChanges,omitempty"`
	SuccessfulTurns int       `json:"successfulTurns"`
	StartedAt       time.Time `json:"startedAt,omitempty"`
	LastObservedAt  time.Time `json:"lastObservedAt,omitempty"`
}

type MatchCompanionState struct {
	UserID             string           `json:"userId"`
	MatchID            string           `json:"matchId"`
	Affect             AffectState      `json:"affect"`
	Initiative         InitiativeBudget `json:"initiative"`
	RecentActions      []ActionRecord   `json:"recentActions,omitempty"`
	RecentPhraseHashes []uint64         `json:"recentPhraseHashes,omitempty"`
	OpenThreads        []OpenThread     `json:"openThreads,omitempty"`
	PlaybackState      string           `json:"playbackState,omitempty"`
	LastFactRevision   string           `json:"lastFactRevision,omitempty"`
	LastSignalID       string           `json:"lastSignalId,omitempty"`
	Version            int64            `json:"version"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

type ActionRecord struct {
	SignalID string             `json:"signalId"`
	Actions  []CommunicationAct `json:"actions"`
	At       time.Time          `json:"at"`
}

type OpenThread struct {
	ID        string     `json:"id"`
	Topic     string     `json:"topic"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type InitiativeBudget struct {
	Mode           string     `json:"mode"`
	LastNormalAt   *time.Time `json:"lastNormalAt,omitempty"`
	LastCriticalAt *time.Time `json:"lastCriticalAt,omitempty"`
}

type StateBundle struct {
	Relationship RelationshipState
	Match        MatchCompanionState
	Memories     []RelationshipMemory
}

type ExpectedVersions struct {
	Relationship int64
	Match        int64
}

type StateUpdate struct {
	Relationship RelationshipState
	Match        MatchCompanionState
	Memories     []RelationshipMemory
	Decision     Decision
}

type Decision struct {
	ID            string               `json:"id"`
	SignalID      string               `json:"signalId"`
	FactRevision  string               `json:"factRevision,omitempty"`
	RefreshCount  int                  `json:"refreshCount,omitempty"`
	StateVersion  int64                `json:"stateVersion"`
	Actions       []CommunicationAct   `json:"actions"`
	Relationship  RelationshipView     `json:"relationship"`
	Presentation  PresentationPlan     `json:"presentation"`
	Speech        *SpeechPlan          `json:"speech,omitempty"`
	Memories      []RelationshipMemory `json:"memories,omitempty"`
	UsedMemoryIDs []string             `json:"usedMemoryIds,omitempty"`
	PlaybackState string               `json:"playbackState,omitempty"`
	ReasonCodes   []string             `json:"reasonCodes"`
	CreatedAt     time.Time            `json:"createdAt"`
}

type CommunicationAct string

const (
	ActAcknowledge CommunicationAct = "ack"
	ActAnalyze     CommunicationAct = "analyze"
	ActAsk         CommunicationAct = "ask"
	ActDisagree    CommunicationAct = "disagree"
	ActOpinion     CommunicationAct = "opinion"
	ActRecall      CommunicationAct = "recall"
	ActReact       CommunicationAct = "react"
	ActRepair      CommunicationAct = "repair"
	ActSilence     CommunicationAct = "silence"
	ActTease       CommunicationAct = "tease"
)

type RelationshipView struct {
	Stage               RelationshipStage `json:"stage"`
	RepairActive        bool              `json:"repairActive"`
	RepairCategory      string            `json:"repairCategory,omitempty"`
	BoundaryCount       int               `json:"boundaryCount"`
	AllowedBanterScopes int               `json:"allowedBanterScopes"`
	GreetingDelivered   bool              `json:"greetingDelivered"`
	InitiativeMode      string            `json:"initiativeMode"`
	AnalysisAppetite    string            `json:"analysisAppetite"`
}

type RelationshipMemory struct {
	ID                 string          `json:"id"`
	UserID             string          `json:"userId"`
	MatchID            string          `json:"matchId,omitempty"`
	Kind               string          `json:"kind"`
	Payload            json.RawMessage `json:"payload"`
	Confidence         float64         `json:"confidence"`
	SourceTraceID      string          `json:"sourceTraceId,omitempty"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"createdAt"`
	LastUsedAt         *time.Time      `json:"lastUsedAt,omitempty"`
	ExpiresAt          *time.Time      `json:"expiresAt,omitempty"`
	PendingDecisionIDs []string        `json:"pendingDecisionIds,omitempty"`
}

const (
	MemoryKindSharedMoment   = "shared_moment"
	MemoryKindOpenThread     = "open_thread"
	MemoryKindBoundary       = "boundary"
	MemoryKindProcedure      = "procedure"
	MemoryKindRitual         = "ritual"
	MemoryKindBanterEvidence = "banter_evidence"
	MemoryKindTasteEvidence  = "taste_evidence"
)

type TasteMemoryPayload struct {
	Subject   string `json:"subject"`
	Direction string `json:"direction"`
}

type BoundaryMemoryPayload struct {
	Scope string `json:"scope"`
	Rule  string `json:"rule"`
}

type ProcedureMemoryPayload struct {
	Category        string   `json:"category"`
	BehaviorChanges []string `json:"behaviorChanges"`
}

type OpenThreadMemoryPayload struct {
	Topic string `json:"topic"`
}

type SharedMomentMemoryPayload struct {
	EventID    string `json:"eventId"`
	EventType  string `json:"eventType"`
	Summary    string `json:"summary"`
	TeamName   string `json:"teamName,omitempty"`
	PlayerName string `json:"playerName,omitempty"`
	RevisionOf string `json:"revisionOf,omitempty"`
}

type AffectState struct {
	Valence    float64   `json:"valence"`
	Arousal    float64   `json:"arousal"`
	Tension    float64   `json:"tension"`
	Confidence float64   `json:"confidence"`
	Engagement float64   `json:"engagement"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

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

type SpeechPlan struct {
	Actions  []CommunicationAct `json:"actions"`
	Content  ContentPolicy      `json:"content"`
	Delivery DeliveryPolicy     `json:"delivery"`
}

type ContentPolicy struct {
	Goal               string   `json:"goal"`
	RequiredAnchors    []string `json:"requiredAnchors,omitempty"`
	ForbiddenClaims    []string `json:"forbiddenClaims,omitempty"`
	ForbiddenTopics    []string `json:"forbiddenTopics,omitempty"`
	MaxSentences       int      `json:"maxSentences"`
	MaxCharacters      int      `json:"maxCharacters"`
	QuestionAllowed    bool     `json:"questionAllowed"`
	AnalysisDepth      string   `json:"analysisDepth"`
	BanterScope        string   `json:"banterScope,omitempty"`
	ProfanityLevel     string   `json:"profanityLevel"`
	Addressing         string   `json:"addressing,omitempty"`
	RecentPhraseHashes []uint64 `json:"recentPhraseHashes,omitempty"`
}

type DeliveryPolicy struct {
	Urgency       string `json:"urgency"`
	InterruptMode string `json:"interruptMode"`
	TTLSeconds    int    `json:"ttlSeconds"`
	DedupeKey     string `json:"dedupeKey,omitempty"`
}
