package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/domain/period"
	"github.com/nanashiwang/meta-pulse/internal/ports"
)

// The signed community BFF supplies durable task receipts, never browser counts.
// Day/category/ordinal form a finite namespace: at most six tickets per user/day.
type CommunityTicket struct {
	Day      string `json:"day"`
	Category string `json:"category"`
	Ordinal  int    `json:"ordinal"`
}
type CommunityTicketResult struct {
	EntryID uint64 `json:"entry_id"`
}
type CommunityTicketService struct {
	unit ports.UnitOfWork
	now  func() time.Time
}

func NewCommunityTicketService(unit ports.UnitOfWork) *CommunityTicketService {
	return &CommunityTicketService{unit: unit, now: time.Now}
}

var communityZone = time.FixedZone("Asia/Shanghai", 8*3600)

func (s *CommunityTicketService) Grant(ctx context.Context, user uint64, task CommunityTicket) (CommunityTicketResult, error) {
	day, err := time.ParseInLocation("2006-01-02", task.Day, communityZone)
	cap := map[string]int{"received": 2, "participation": 3, "checkin": 1}[task.Category]
	if user == 0 || err != nil || day.Format("2006-01-02") != task.Day || day.After(s.now()) || task.Ordinal < 1 || task.Ordinal > cap {
		return CommunityTicketResult{}, ports.ErrConflict
	}
	key := fmt.Sprintf("community-v1:%d:%s:%s:%d", user, task.Day, task.Category, task.Ordinal)
	hash := sha256.Sum256([]byte(key))
	fingerprint := hex.EncodeToString(hash[:])
	var result CommunityTicketResult
	err = s.unit.Do(ctx, func(r ports.Repositories) error {
		// Same ordering as new Actions and rule publication; no period in the key.
		if e := lockRewardRule(ctx, r.Idempotency); e != nil {
			return e
		}
		receipt, e := r.Idempotency.GetOrCreateForUpdate(ctx, "community_ticket", key, fingerprint)
		if e != nil {
			return e
		}
		if receipt.PayloadHash != fingerprint {
			return ledger.ErrIdempotencyConflict
		}
		if len(receipt.ResponseJSON) > 0 {
			return json.Unmarshal(receipt.ResponseJSON, &result)
		}
		now := s.now().UTC().Truncate(time.Second)
		p, e := r.Period.FindActiveAtCurrent(ctx, now)
		if e != nil {
			return e
		}
		if !p.Continuous || p.FundingPolicy != period.VerifiedPaidFunding || p.QuotaValidityDays < 1 {
			return errors.New("community tickets require current continuous rules")
		}
		if e = r.Tickets.LockUser(ctx, user); e != nil {
			return e
		}
		expires := now.Add(time.Duration(p.QuotaValidityDays) * 24 * time.Hour)
		meta, _ := json.Marshal(map[string]any{"task": task, "ticket_lot_version": 1, "earned_at": now.Unix(), "quota_expires_at": expires.Unix()})
		entry, e := appendEntry(ctx, r, ledger.Entry{UserID: user, PeriodID: p.ID, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketBonus, Amount: 1, SourceType: "community_task", SourceRef: key, IdempotencyKey: key, PayloadHash: fingerprint, Reason: "社区互动与签到赠券", MetadataJSON: meta, CreatedAt: now})
		if e != nil {
			return e
		}
		if e = r.Tickets.Mint(ctx, ports.TicketLot{UserID: user, PeriodID: p.ID, MintEntryID: entry.ID, Issued: 1, Remaining: 1, EarnedAt: now, QuotaExpiresAt: expires}); e != nil {
			return e
		}
		result.EntryID = entry.ID
		response, _ := json.Marshal(result)
		if e = r.Audit.Append(ctx, ports.AuditLog{ActorType: "community-bff", ActorID: fmt.Sprint(user), Action: "community_ticket", ResourceType: "ledger_entry", ResourceID: fmt.Sprint(entry.ID), Reason: entry.Reason, AfterJSON: meta, RequestID: key, CreatedAt: now}); e != nil {
			return e
		}
		status := 200
		receipt.ResponseStatus = &status
		receipt.ResponseJSON = response
		receipt.ResourceType = "ledger_entry"
		receipt.ResourceID = fmt.Sprint(entry.ID)
		return r.Idempotency.Save(ctx, receipt)
	})
	return result, err
}
