package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
)

const (
	OutboxStatusProcessing = "processing"
	OutboxStatusRetry      = "retry"
	OutboxStatusCompleted  = "completed"
	OutboxStatusDead       = "dead"
	GrantStatusSettled     = "settled"
	GrantStatusDead        = "settlement_dead"
	GrantStatusReversed    = "reversed"
)

var (
	ErrInvalidSettlementPayload = errors.New("invalid settlement payload")
	ErrSettlementConflict       = errors.New("settlement source conflict")
)

type SettlementConfig struct {
	BatchSize   int
	Lease       time.Duration
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	MaxAttempts uint32
	Now         func() time.Time
}

type SettlementReport struct {
	Claimed   int `json:"claimed"`
	Completed int `json:"completed"`
	Retried   int `json:"retried"`
	Dead      int `json:"dead"`
	Failed    int `json:"failed"`
}

type SettlementService struct {
	unit   ports.UnitOfWork
	client ports.BenefitClient
	cfg    SettlementConfig
}

func NewSettlementService(unit ports.UnitOfWork, client ports.BenefitClient, cfg SettlementConfig) (*SettlementService, error) {
	if unit == nil || client == nil {
		return nil, errors.New("settlement dependencies are nil")
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 5000 {
		return nil, errors.New("settlement batch size must be between 1 and 5000")
	}
	if cfg.Lease <= 0 {
		cfg.Lease = 2 * time.Minute
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 5 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 15 * time.Minute
	}
	if cfg.MaxBackoff < cfg.BaseBackoff {
		return nil, errors.New("settlement max backoff must not be below base backoff")
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &SettlementService{unit: unit, client: client, cfg: cfg}, nil
}

func (s *SettlementService) ProcessBatch(ctx context.Context) (SettlementReport, error) {
	var report SettlementReport
	var outboxes []ports.SettlementOutbox
	now := s.cfg.Now()
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Settlement == nil {
			return errors.New("settlement repository is not initialized")
		}
		var err error
		outboxes, err = repos.Settlement.ClaimDue(ctx, now, s.cfg.BatchSize, now.Add(s.cfg.Lease))
		return err
	})
	if err != nil {
		return report, err
	}
	report.Claimed = len(outboxes)
	for _, outbox := range outboxes {
		status, err := s.processOne(ctx, outbox)
		if err != nil {
			report.Failed++
			continue
		}
		switch status {
		case OutboxStatusCompleted:
			report.Completed++
		case OutboxStatusRetry:
			report.Retried++
		case OutboxStatusDead:
			report.Dead++
		}
	}
	return report, nil
}

func (s *SettlementService) processOne(ctx context.Context, outbox ports.SettlementOutbox) (string, error) {
	grant, request, err := s.loadSettlement(ctx, outbox)
	if err != nil {
		return s.failPayload(ctx, outbox, err)
	}
	response, grantErr := s.client.Grant(ctx, request)
	if grantErr == nil && response.Applied && sameSourceRef(response.SourceRef, grant.SourceRef) {
		if err := s.complete(ctx, outbox, grant); err != nil {
			return "", err
		}
		return OutboxStatusCompleted, nil
	}

	// A timeout or any ambiguous response must query the original source_ref
	// before retrying. The source_ref is never regenerated.
	state, queryErr := s.client.Query(ctx, grant.SourceRef)
	if queryErr == nil && state.Applied && sameSourceRef(state.SourceRef, grant.SourceRef) {
		if err := s.complete(ctx, outbox, grant); err != nil {
			return "", err
		}
		return OutboxStatusCompleted, nil
	}
	if grantErr != nil && isBenefitConflict(grantErr) {
		return s.failPayload(ctx, outbox, fmt.Errorf("%w: %v", ErrSettlementConflict, grantErr))
	}
	reason := grantErr
	if reason == nil {
		reason = errors.New("benefit API did not confirm application")
	}
	if queryErr != nil {
		reason = fmt.Errorf("%v; query: %w", reason, queryErr)
	} else if !state.Applied {
		reason = fmt.Errorf("%v; benefit not applied", reason)
	}
	return s.retry(ctx, outbox, reason)
}

func (s *SettlementService) loadSettlement(ctx context.Context, outbox ports.SettlementOutbox) (ports.RewardGrant, ports.BenefitGrantRequest, error) {
	var grant ports.RewardGrant
	var payload settlementPayload
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Reward == nil {
			return errors.New("reward repository is not initialized")
		}
		found, err := repos.Reward.FindGrantByID(ctx, outbox.RewardGrantID)
		if err != nil {
			return err
		}
		grant = *found
		return nil
	})
	if err != nil {
		return ports.RewardGrant{}, ports.BenefitGrantRequest{}, err
	}
	if grant.GrantID == "" || grant.SourceRef == "" || grant.UserID == 0 || grant.Amount <= 0 || strings.TrimSpace(grant.BudgetType) == "" || grant.Status != RewardStatusPending {
		return ports.RewardGrant{}, ports.BenefitGrantRequest{}, ErrInvalidSettlementPayload
	}
	// MySQL JSON columns may reorder object keys and normalize whitespace on
	// read. Verify the canonical semantic JSON hash first, while accepting the
	// legacy struct-order hash so rows written before this fix remain recoverable.
	canonicalHash := canonicalJSONHash(outbox.PayloadJSON)
	legacyHash := settlementLegacyPayloadHash(outbox.PayloadJSON)
	if outbox.PayloadHash == "" || (canonicalHash != outbox.PayloadHash && legacyHash != outbox.PayloadHash) {
		return ports.RewardGrant{}, ports.BenefitGrantRequest{}, ErrInvalidSettlementPayload
	}
	if err := decodeStrictJSON(outbox.PayloadJSON, &payload); err != nil {
		return ports.RewardGrant{}, ports.BenefitGrantRequest{}, fmt.Errorf("%w: %v", ErrInvalidSettlementPayload, err)
	}
	if payload.GrantID != grant.GrantID || payload.GrantID != payload.SourceRef || payload.UserID != grant.UserID || payload.Amount != grant.Amount || payload.SourceRef != grant.SourceRef || payload.TransferableQuota || payload.RewardType != grant.RewardType {
		return ports.RewardGrant{}, ports.BenefitGrantRequest{}, ErrInvalidSettlementPayload
	}
	return grant, ports.BenefitGrantRequest{GrantID: payload.GrantID, UserID: payload.UserID, Amount: payload.Amount, TransferableQuota: false, SourceRef: payload.SourceRef, RewardType: payload.RewardType, PayloadHash: outbox.PayloadHash}, nil
}

type settlementPayload struct {
	GrantID           string `json:"grant_id"`
	UserID            uint64 `json:"user_id"`
	Amount            int64  `json:"amount"`
	TransferableQuota bool   `json:"transferable_quota"`
	SourceRef         string `json:"source_ref"`
	RewardType        string `json:"reward_type"`
}

func (s *SettlementService) complete(ctx context.Context, outbox ports.SettlementOutbox, grant ports.RewardGrant) error {
	now := s.cfg.Now()
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Reward == nil || repos.Settlement == nil {
			return errors.New("settlement repositories are not initialized")
		}
		current, err := repos.Reward.FindGrantByID(ctx, grant.ID)
		if err != nil {
			return err
		}
		if current.Status != GrantStatusSettled {
			budget, err := repos.Reward.GetBudgetForUpdate(ctx, current.PeriodID, current.BudgetType)
			if err != nil {
				return err
			}
			if current.Amount < 0 || budget.ReservedAmount < current.Amount || budget.Version == math.MaxUint64 {
				return errors.New("settlement budget reservation is inconsistent")
			}
			budget.ReservedAmount -= current.Amount
			if budget.SettledAmount > math.MaxInt64-current.Amount {
				return errors.New("settlement budget overflow")
			}
			budget.SettledAmount += current.Amount
			budget.Version++
			if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
				return err
			}
			if err := repos.Reward.UpdateGrantStatus(ctx, current.ID, GrantStatusSettled, &now, nil); err != nil {
				return err
			}
		}
		outbox.Status = OutboxStatusCompleted
		outbox.LeasedUntil = nil
		outbox.LastError = ""
		outbox.CompletedAt = &now
		return repos.Settlement.SaveOutbox(ctx, outbox)
	})
}

func (s *SettlementService) retry(ctx context.Context, outbox ports.SettlementOutbox, reason error) (string, error) {
	now := s.cfg.Now()
	status := OutboxStatusRetry
	if outbox.Attempts >= s.cfg.MaxAttempts {
		status = OutboxStatusDead
	}
	outbox.Status = status
	outbox.LeasedUntil = nil
	outbox.LastError = truncateError(reason)
	outbox.NextAttemptAt = now.Add(s.backoff(outbox.Attempts))
	if status == OutboxStatusDead {
		outbox.NextAttemptAt = now
	}
	if err := s.saveOutbox(ctx, outbox); err != nil {
		return "", err
	}
	return status, nil
}

func (s *SettlementService) failPayload(ctx context.Context, outbox ports.SettlementOutbox, reason error) (string, error) {
	// Invalid data is not retriable: retrying could send a tampered payload.
	now := s.cfg.Now()
	outbox.Status = OutboxStatusDead
	outbox.LeasedUntil = nil
	outbox.NextAttemptAt = now
	outbox.LastError = truncateError(reason)
	if err := s.saveOutbox(ctx, outbox); err != nil {
		return "", err
	}
	return OutboxStatusDead, nil
}

func (s *SettlementService) saveOutbox(ctx context.Context, outbox ports.SettlementOutbox) error {
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Settlement == nil {
			return errors.New("settlement repository is not initialized")
		}
		return repos.Settlement.SaveOutbox(ctx, outbox)
	})
}

func (s *SettlementService) backoff(attempts uint32) time.Duration {
	result := s.cfg.BaseBackoff
	for i := uint32(1); i < attempts && result < s.cfg.MaxBackoff; i++ {
		if result > s.cfg.MaxBackoff/2 {
			return s.cfg.MaxBackoff
		}
		result *= 2
	}
	if result > s.cfg.MaxBackoff {
		return s.cfg.MaxBackoff
	}
	return result
}

type ReconciliationReport struct {
	Checked   int `json:"checked"`
	Settled   int `json:"settled"`
	Unchanged int `json:"unchanged"`
	Failed    int `json:"failed"`
}

// Reconcile queries the original source_ref for non-terminal outbox records.
// It is safe to run repeatedly and never creates a new grant or source_ref.
func (s *SettlementService) Reconcile(ctx context.Context) (ReconciliationReport, error) {
	var report ReconciliationReport
	var outboxes []ports.SettlementOutbox
	err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Settlement == nil {
			return errors.New("settlement repository is not initialized")
		}
		var err error
		outboxes, err = repos.Settlement.ListForReconciliation(ctx, s.cfg.BatchSize)
		return err
	})
	if err != nil {
		return report, err
	}
	for _, outbox := range outboxes {
		report.Checked++
		var grant *ports.RewardGrant
		err := s.unit.Do(ctx, func(repos ports.Repositories) error {
			if repos.Reward == nil {
				return errors.New("reward repository is not initialized")
			}
			found, err := repos.Reward.FindGrantByID(ctx, outbox.RewardGrantID)
			if err == nil {
				grant = found
			}
			return err
		})
		if err != nil || grant == nil {
			if err != nil {
				report.Failed++
			} else {
				report.Unchanged++
			}
			continue
		}
		state, queryErr := s.client.Query(ctx, grant.SourceRef)
		if queryErr != nil || !state.Applied || !sameSourceRef(state.SourceRef, grant.SourceRef) {
			report.Unchanged++
			if queryErr != nil {
				report.Failed++
			}
			continue
		}
		if err := s.complete(ctx, outbox, *grant); err != nil {
			report.Failed++
			continue
		}
		report.Settled++
	}
	return report, nil
}

func (s *SettlementService) Rollback(ctx context.Context, grantID uint64, reason string) error {
	if grantID == 0 || strings.TrimSpace(reason) == "" {
		return errors.New("invalid settlement rollback request")
	}
	var grant ports.RewardGrant
	var alreadyReversed bool
	if err := s.unit.Do(ctx, func(repos ports.Repositories) error {
		if repos.Reward == nil {
			return errors.New("reward repository is not initialized")
		}
		var found *ports.RewardGrant
		var err error
		if locker, ok := repos.Reward.(interface {
			FindGrantByIDForUpdate(context.Context, uint64) (*ports.RewardGrant, error)
		}); ok {
			found, err = locker.FindGrantByIDForUpdate(ctx, grantID)
		} else {
			found, err = repos.Reward.FindGrantByID(ctx, grantID)
		}
		if err != nil {
			return err
		}
		grant = *found
		if grant.Status == GrantStatusReversed {
			alreadyReversed = true
			return nil
		}
		if grant.Status != GrantStatusSettled {
			return errors.New("only settled grant can be rolled back")
		}
		return nil
	}); err != nil {
		return err
	}
	if alreadyReversed {
		return nil
	}
	state, err := s.client.Rollback(ctx, grant.SourceRef, reason)
	if err != nil || !state.Applied || !sameSourceRef(state.SourceRef, grant.SourceRef) {
		if err != nil {
			return err
		}
		return ErrSettlementConflict
	}
	now := s.cfg.Now()
	return s.unit.Do(ctx, func(repos ports.Repositories) error {
		// The external rollback is idempotent by source_ref, but the local
		// budget mutation must happen exactly once. Re-lock and re-check the
		// grant after the network call so concurrent retries cannot release the
		// same reserved amount twice.
		var current *ports.RewardGrant
		var err error
		if locker, ok := repos.Reward.(interface {
			FindGrantByIDForUpdate(context.Context, uint64) (*ports.RewardGrant, error)
		}); ok {
			current, err = locker.FindGrantByIDForUpdate(ctx, grant.ID)
		} else {
			current, err = repos.Reward.FindGrantByID(ctx, grant.ID)
		}
		if err != nil {
			return err
		}
		if current.Status == GrantStatusReversed {
			return nil
		}
		if current.Status != GrantStatusSettled {
			return errors.New("only settled grant can be rolled back")
		}
		budget, err := repos.Reward.GetBudgetForUpdate(ctx, current.PeriodID, current.BudgetType)
		if err != nil {
			return err
		}
		if budget.SettledAmount < current.Amount || budget.Version == math.MaxUint64 {
			return errors.New("settlement rollback budget is inconsistent")
		}
		budget.SettledAmount -= current.Amount
		if budget.ReleasedAmount > math.MaxInt64-current.Amount {
			return errors.New("settlement rollback budget overflow")
		}
		budget.ReleasedAmount += current.Amount
		budget.Version++
		if err := repos.Reward.SaveBudget(ctx, budget); err != nil {
			return err
		}
		if err := repos.Reward.UpdateGrantStatus(ctx, current.ID, GrantStatusReversed, nil, &now); err != nil {
			return err
		}
		return nil
	})
}

func sameSourceRef(actual, expected string) bool { return expected != "" && actual == expected }
func isBenefitConflict(err error) bool           { return errors.Is(err, ports.ErrBenefitPayloadConflict) }
func sha256Hex(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// decodeStrictJSON accepts exactly one JSON value. Settlement payloads come
// from Pulse's database, but rejecting trailing values keeps a corrupted or
// manually altered outbox row from being partially interpreted.
func decodeStrictJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

// canonicalJSONHash hashes JSON after decoding and re-encoding it.
// encoding/json sorts object keys, making the hash stable across MySQL JSON
// key ordering and whitespace normalization.
func canonicalJSONHash(payload []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return sha256Hex(payload)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return sha256Hex(payload)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return sha256Hex(payload)
	}
	return sha256Hex(canonical)
}

// settlementLegacyPayloadHash supports outbox rows written with the original
// struct-field order before payload hashes became canonical. Re-marshalling
// the known payload shape reconstructs that historical byte representation
// even after MySQL has normalized the JSON column.
func settlementLegacyPayloadHash(payload []byte) string {
	var value settlementPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return ""
	}
	legacy, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return sha256Hex(legacy)
}
func truncateError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 1000 {
		return value[:1000]
	}
	return value
}
