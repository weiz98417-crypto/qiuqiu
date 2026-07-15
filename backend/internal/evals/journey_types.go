package evals

import (
	"time"

	"qiuqiu/internal/relationship"
)

type RelationshipJourney struct {
	ID    string                    `json:"id"`
	Steps []RelationshipJourneyStep `json:"steps"`
}

type RelationshipJourneyStep struct {
	ID     string                         `json:"id"`
	At     time.Time                      `json:"at"`
	Signal relationship.Signal            `json:"signal"`
	Expect RelationshipJourneyExpectation `json:"expect"`
}

type RelationshipJourneyExpectation struct {
	Stage            relationship.RelationshipStage  `json:"stage,omitempty"`
	RepairActive     *bool                           `json:"repairActive,omitempty"`
	BoundaryCount    *int                            `json:"boundaryCount,omitempty"`
	RequiredActions  []relationship.CommunicationAct `json:"requiredActions,omitempty"`
	ForbiddenActions []relationship.CommunicationAct `json:"forbiddenActions,omitempty"`
	MemoryKinds      []string                        `json:"memoryKinds,omitempty"`
}

type RelationshipJourneyResult struct {
	ID        string                  `json:"id"`
	Passed    bool                    `json:"passed"`
	Failures  []string                `json:"failures,omitempty"`
	Decisions []relationship.Decision `json:"decisions"`
}
