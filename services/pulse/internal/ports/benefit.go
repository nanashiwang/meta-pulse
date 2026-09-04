package ports

import (
	"context"
	"errors"
)

var ErrBenefitPayloadConflict = errors.New("new-api benefit payload conflict")

const (
	BenefitStatusApplied        = "applied"
	BenefitStatusAlreadyApplied = "already_applied"
	BenefitStatusRolledBack     = "rolled_back"
	BenefitStatusNotFound       = "not_found"
)

type BenefitGrantRequest struct {
	GrantID           string `json:"grant_id"`
	UserID            uint64 `json:"user_id"`
	Amount            int64  `json:"amount"`
	TransferableQuota bool   `json:"transferable_quota"`
	SourceRef         string `json:"source_ref"`
	RewardType        string `json:"reward_type"`
	PayloadHash       string `json:"payload_hash"`
}

type BenefitGrantResponse struct {
	Applied    bool
	RolledBack bool
	Status     string
	SourceRef  string
}

type BenefitState struct {
	Applied    bool
	RolledBack bool
	Status     string
	SourceRef  string
}

type BenefitClient interface {
	Grant(context.Context, BenefitGrantRequest) (BenefitGrantResponse, error)
	Query(context.Context, string) (BenefitState, error)
	Rollback(context.Context, string, string) (BenefitState, error)
}
