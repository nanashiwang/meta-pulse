package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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

// AdminTicketGrant is a reviewed manual gift from a community administrator.
// It mints one ticket lot through the same ledger, lot and audit path as task
// tickets; the community-task daily caps do not apply to manual gifts.
type AdminTicketGrant struct {
	UserID uint64 `json:"user_id"`
	Count  int64  `json:"count"`
	Reason string `json:"reason"`
}

type AdminTicketGrantResult struct {
	EntryID        uint64    `json:"entry_id"`
	Count          int64     `json:"count"`
	QuotaExpiresAt time.Time `json:"quota_expires_at"`
}

const MaxAdminTicketGrant = 100

func (s *CommunityTicketService) AdminGrant(ctx context.Context, actor, requestID string, grant AdminTicketGrant) (AdminTicketGrantResult, error) {
	reason := strings.TrimSpace(grant.Reason)
	if actor == "" || requestID == "" || grant.UserID == 0 || grant.Count < 1 || grant.Count > MaxAdminTicketGrant || utf8.RuneCountInString(reason) < 2 || utf8.RuneCountInString(reason) > 200 {
		return AdminTicketGrantResult{}, ErrInvalidAdminTicketGrant
	}
	payload, _ := json.Marshal(struct {
		Actor  string `json:"actor"`
		UserID uint64 `json:"user_id"`
		Count  int64  `json:"count"`
		Reason string `json:"reason"`
	}{actor, grant.UserID, grant.Count, reason})
	hash := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(hash[:])
	key := "admin-ticket-grant:" + requestID
	var result AdminTicketGrantResult
	err := s.unit.Do(ctx, func(r ports.Repositories) error {
		if e := lockRewardRule(ctx, r.Idempotency); e != nil {
			return e
		}
		receipt, e := r.Idempotency.GetOrCreateForUpdate(ctx, "admin_ticket_grant", requestID, fingerprint)
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
			return errors.New("admin ticket grants require current continuous rules")
		}
		if e = r.Tickets.LockUser(ctx, grant.UserID); e != nil {
			return e
		}
		expires := now.Add(time.Duration(p.QuotaValidityDays) * 24 * time.Hour)
		meta, _ := json.Marshal(map[string]any{"admin_grant": true, "actor": actor, "count": grant.Count, "ticket_lot_version": 1, "earned_at": now.Unix(), "quota_expires_at": expires.Unix()})
		entry, e := appendEntry(ctx, r, ledger.Entry{UserID: grant.UserID, PeriodID: p.ID, AssetType: ledger.AssetTicket, Operation: ledger.OperationTicketBonus, Amount: grant.Count, SourceType: "admin_ticket_grant", SourceRef: requestID, IdempotencyKey: key, PayloadHash: fingerprint, Reason: "管理员赠券：" + reason, MetadataJSON: meta, CreatedAt: now})
		if e != nil {
			return e
		}
		if e = r.Tickets.Mint(ctx, ports.TicketLot{UserID: grant.UserID, PeriodID: p.ID, MintEntryID: entry.ID, Issued: grant.Count, Remaining: grant.Count, EarnedAt: now, QuotaExpiresAt: expires}); e != nil {
			return e
		}
		result = AdminTicketGrantResult{EntryID: entry.ID, Count: grant.Count, QuotaExpiresAt: expires}
		response, _ := json.Marshal(result)
		after, _ := json.Marshal(map[string]any{"user_id": grant.UserID, "count": grant.Count, "balance_after": entry.BalanceAfter, "quota_expires_at": expires.Unix()})
		if e = r.Audit.Append(ctx, ports.AuditLog{ActorType: "community_admin", ActorID: actor, Action: "admin_ticket_grant", ResourceType: "ledger_entry", ResourceID: fmt.Sprint(entry.ID), Reason: reason, AfterJSON: after, RequestID: requestID, CreatedAt: now}); e != nil {
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

var ErrInvalidAdminTicketGrant = errors.New("invalid admin ticket grant")
