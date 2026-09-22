// Package memory is the in-domain memory seam from ADR-0006: observations,
// recall and the synthesized user portrait live behind one interface so the
// Memobase adapter can be swapped without touching the companion kernel.
// The Interaction Ledger stays the immutable fact stream; nothing here
// creates, corrects, or asserts Match Facts.
package memory

import (
	"context"
	"errors"
	"time"
)

// MomentKind classifies why an observation is worth remembering.
type MomentKind string

const (
	MomentUserFact          MomentKind = "user_fact"
	MomentEmotionalExchange MomentKind = "emotional_exchange"
	MomentPromise           MomentKind = "promise"
	MomentMatchEvent        MomentKind = "match_event"
)

// Stage mirrors the relationship stages; kept as a local type so the memory
// seam does not import the relationship package.
type Stage string

const (
	StageNone        Stage = ""
	StageFirstMeeting Stage = "first_meeting"
	StageFamiliar     Stage = "familiar"
	StageWatchBuddy   Stage = "watch_buddy"
	StageOldBallmate  Stage = "old_ballmate"
)

// Moment is one auditable observation. Importance is assigned at enqueue time
// by ScoreImportance and is never rewritten by Memobase synthesis (ADR-0006).
// LedgerSequence cites the fact ledger for provenance (0 when the source has
// no numeric sequence, e.g. plain conversation turns). MatchID attributes the
// moment to the match it was observed under (reflection-attribution: post-match
// reflection labels the audit with the match the user actually watched).
type Moment struct {
	UserID         string
	Kind           MomentKind
	Content        string
	Importance     float64
	LedgerSequence int64
	OccurredAt     time.Time
	MatchID        string
}

// Query selects memories for context assembly. Focus is the raw user text or
// current topic used for relevance scoring.
type Query struct {
	UserID string
	Focus  string
	Limit  int
}

// Recall is one retrieved memory. Source carries provenance (e.g.
// "memobase://profile/basic_info/favorite_team") so recall blocks can cite it.
type Recall struct {
	Content    string
	Importance float64
	OccurredAt time.Time
	Source     string
}

// Portrait is the synthesized user model rendered as a bounded Chinese block
// ready for prompt injection. Entries carries the structured slots so the C3
// user page (球球懂我) reads exactly what the prompt injects — one source, no
// decorative copy.
type Portrait struct {
	Block     string
	UpdatedAt time.Time
	Entries   []PortraitEntry
}

// Where a portrait slot's content comes from: Memobase synthesis or an
// explicit user edit recorded in the local overlay table (migrations/041).
const (
	PortraitSourceSynthesis = "synthesis"
	PortraitSourceUser      = "user"
)

// PortraitEntry is one structured profile slot. ID is the Memobase-side
// profile id when the slot exists in remote synthesis (used to forward user
// edits/deletes to Memobase); local overlay slots have no remote id.
type PortraitEntry struct {
	ID        string
	Topic     string
	SubTopic  string
	Content   string
	UpdatedAt time.Time
	Source    string
}

// ProfileEntryMutator is implemented by adapters whose remote profile slots
// can be edited and deleted in place (Memobase: PUT/DELETE
// /users/profile/{id}/{profileID} in the official SDK). The Queue best-effort
// forwards user mutations after the local overlay write has already decided
// what the next turn sees.
type ProfileEntryMutator interface {
	UpdateProfileEntry(ctx context.Context, userID, entryID, topic, subTopic, content string) error
	DeleteProfileEntry(ctx context.Context, userID, entryID string) error
}

// ThreadKind mirrors the CONTEXT.md open-thread taxonomy, persisted in the
// local open_threads table (migrations/040).
type ThreadKind string

const (
	ThreadUnansweredQuestion ThreadKind = "unanswered_question"
	ThreadPromise            ThreadKind = "promise"
	ThreadEmotionalMoment    ThreadKind = "emotional_moment"
	ThreadPrediction         ThreadKind = "prediction"
	// ThreadUnroutable is the intent-router C3 funnel (ADR-0009): every
	// unknown turn opens one, question or not, so the operator overview can
	// see what the companion failed to understand. Recovery beats never
	// speak into an unroutable thread — it is vocabulary material, not a
	// callback loop.
	ThreadUnroutable ThreadKind = "unroutable"
)

// Thread is an open loop worth a later callback. SourceTurn cites the
// originating interaction (signal id or event id) and LedgerSequence cites
// the fact ledger when the source was a recorded match event — provenance
// discipline identical to Moment.
type Thread struct {
	ID             string
	UserID         string
	Kind           ThreadKind
	Content        string
	State          string
	SourceTurn     string
	LedgerSequence int64
	CreatedAt      time.Time
}

var (
	// ErrNotSupported marks operations an adapter does not implement (e.g.
	// Threads on the Memobase adapter before the local C2 store exists).
	ErrNotSupported = errors.New("memory: not supported by this adapter")
	// ErrUnavailable marks a degraded adapter (unconfigured, unreachable, or
	// recently failing); callers fall back to read_recent instead of failing.
	ErrUnavailable = errors.New("memory: adapter unavailable")
	// ErrNotFound marks a requested row (thread id) that does not exist.
	ErrNotFound = errors.New("memory: row not found")
)
