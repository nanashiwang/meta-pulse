// Package runtimeconfig owns audited, role-separated operational settings.
// It deliberately excludes period economics and the reward random seed.
package runtimeconfig

import (
	"context"
	"errors"
)

const (
	RoleAPI        = "api"
	RoleWorker     = "worker"
	MaxSafeInteger = int64(1<<53 - 1)
)

var (
	ErrInvalid     = errors.New("invalid runtime settings")
	ErrConflict    = errors.New("runtime settings conflict")
	ErrKeyMismatch = errors.New("runtime encryption key mismatch: restore the original role key volume")
)

// Patch preserves absence so unset values continue to use process environment.
type Patch struct {
	NewAPIInternalBaseURL *string `json:"newapi_internal_base_url,omitempty"`
	QuotaPerUnit          *string `json:"quota_per_unit,omitempty"`
	ActionsEnabled        *bool   `json:"actions_enabled,omitempty"`
	RewardShadowMode      *bool   `json:"reward_shadow_mode,omitempty"`
}

type PublicConfig struct {
	NewAPIInternalBaseURL string `json:"newapi_internal_base_url"`
	QuotaPerUnit          string `json:"quota_per_unit"`
	ActionsEnabled        bool   `json:"actions_enabled"`
	RewardShadowMode      bool   `json:"reward_shadow_mode"`
}

type SecretStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
}

type View struct {
	Revision           uint64                  `json:"revision"`
	Config             PublicConfig            `json:"config"`
	Secrets            map[string]SecretStatus `json:"secrets"`
	WorkerReady        bool                    `json:"worker_ready"`
	NewAPITargetLocked bool                    `json:"newapi_target_locked"`
}

type UpdateRequest struct {
	Revision     uint64            `json:"revision"`
	Config       *Patch            `json:"config,omitempty"`
	Secrets      map[string]string `json:"secrets,omitempty"`
	ClearSecrets []string          `json:"clear_secrets,omitempty"`
	Reason       string            `json:"reason"`
}

// Storage types never appear in HTTP responses. Fingerprints permit duplicate
// detection without exposing another process role's plaintext credentials.
type Secret struct {
	Role        string
	Ciphertext  []byte
	Fingerprint string
}

type Registration struct {
	Role                    string
	PublicKey               []byte
	EnvironmentFingerprints map[string]string
	NewAPIInternalBaseURL   string
}

type Record struct {
	Revision   uint64
	HasRewards bool
	Config     Patch
	Secrets    map[string]Secret
	Roles      map[string]Registration
}

type WriteRequest struct {
	Revision    uint64
	ActorID     string
	RequestID   string
	PayloadHash string
	Reason      string
}

type Mutation struct {
	Config        Patch
	Secrets       map[string]Secret
	ChangedFields []string
	Before        View
	After         View
}

type Store interface {
	Register(context.Context, Registration) error
	// Read returns ciphertext only for the named process role. The API can
	// read worker fingerprints and public keys but never its encrypted value.
	Read(context.Context, string) (Record, error)
	// Update serializes registration and settings changes on one durable
	// row. Idempotency, CAS, audit and mutations commit together.
	Update(context.Context, WriteRequest, func(Record) (Mutation, error)) (View, error)
}
