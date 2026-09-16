// Package memory is the in-domain memory seam from ADR-0006: observations,
// recall and the synthesized user portrait live behind one interface so the
// Memobase adapter can be swapped without touching the companion kernel.
// The Interaction Ledger stays the immutable fact stream; nothing here
// creates, corrects, or asserts Match Facts.
package memory

import (
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
// no numeric sequence, e.g. plain conversation turns).
type Moment struct {
	UserID         string
	Kind           MomentKind
	Content        string
	Importance     float64
	LedgerSequence int64
	OccurredAt     time.Time
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
// ready for prompt injection (C3 wires the client-facing page on top).
type Portrait struct {
	Block     string
	UpdatedAt time.Time
}

// ThreadKind mirrors the CONTEXT.md open-thread taxonomy, persisted in the
// local open_threads table (migrations/040).
type ThreadKind string

const (
	ThreadUnansweredQuestion ThreadKind = "unanswered_question"
	ThreadPromise            ThreadKind = "promise"
	ThreadEmotionalMoment    ThreadKind = "emotional_moment"
	ThreadPrediction         ThreadKind = "prediction"
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
)
