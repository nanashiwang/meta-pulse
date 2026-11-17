package pulse_user_center

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/communitytickets"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/growth"
	"github.com/segmentfault/pacman/log"
)

type flashResponse struct {
	GrantID string `json:"grant_id"`
	Cents   int64  `json:"cents"`
	Status  string `json:"status"`
}

func (uc *UserCenter) deliverCheckinFlash(ctx context.Context, s *communitytickets.Store, v communitytickets.FlashReceipt, create bool) error {
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	reader, ok := uc.Guard.(communityIdentityReader)
	if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.CommunityBFFHMACSecret) {
		return errors.New("check-in flash binding unavailable")
	}
	if err := uc.Guard.Ready(ctx); err != nil {
		return err
	}
	external, err := reader.CommunityIdentity(ctx, v.UserID)
	if err != nil {
		return err
	}
	if external != v.ExternalID {
		return errors.New("check-in flash binding changed")
	}
	method, path := http.MethodGet, "/v1/internal/me/checkin-flash/"+url.PathEscape(v.GrantID)
	var body []byte
	if v.GrantID == "" {
		if !create {
			return nil
		}
		method, path = http.MethodPost, "/v1/internal/me/checkin-flash"
		body, _ = json.Marshal(struct {
			Day     string `json:"day"`
			Cents   int64  `json:"cents"`
			EventID uint64 `json:"event_id"`
		}{v.Day, v.Cents, v.EventID})
	}
	data, status, err := uc.Client.communityRequest(ctx, method, path, external, "", body)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("check-in flash upstream status %d", status)
	}
	var result flashResponse
	if err = decodeCommunityResponse(data, &result); err != nil {
		return err
	}
	return s.SaveFlash(ctx, v, result.GrantID, result.Status, result.Cents)
}
func (uc *UserCenter) checkinFlashBatch(ctx context.Context, s *communitytickets.Store) error {
	collectErr := s.CollectFlash(ctx)
	rows, err := s.PendingFlash(ctx)
	if err != nil {
		return err
	}
	for _, v := range rows {
		if err = s.DeferFlash(ctx, v); err != nil {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
		if err := uc.deliverCheckinFlash(attempt, s, v, true); err != nil {
			log.Warnf("check-in flash delivery deferred; event=%d saved_amount_retained=true", v.EventID)
		}
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return collectErr
}

// Flash is independent of EXP: a Pulse failure never rolls back a completed
// check-in. Both immediate submission and background retries use saved cents.
func (uc *UserCenter) enrichCheckinFlash(ctx context.Context, user string, summary *growth.Summary, prepare bool) {
	if summary.CheckinReceipt == nil || summary.CheckinReceipt.Day < communitytickets.FlashStartDay {
		return
	}
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	s, err := uc.communityTicketStore(c)
	if err != nil {
		summary.CheckinReceipt.FlashStatus = "unavailable"
		return
	}
	if prepare {
		if err = s.PrepareFlash(c, user); err != nil {
			summary.CheckinReceipt.FlashStatus = "unavailable"
			return
		}
	}
	v, err := s.Flash(c, user, summary.CheckinReceipt.Day)
	if errors.Is(err, sql.ErrNoRows) {
		eligible, e := s.FlashEligible(c, user, summary.CheckinReceipt.Day)
		if e != nil {
			summary.CheckinReceipt.FlashStatus = "unavailable"
		} else if eligible {
			summary.CheckinReceipt.FlashStatus = "preparing"
			summary.CheckinReceipt.FlashAmount = ""
		}
		return
	}
	if err != nil {
		summary.CheckinReceipt.FlashStatus = "unavailable"
		return
	}
	if v.GrantID != "" || prepare {
		syncErr := uc.deliverCheckinFlash(c, s, v, prepare)
		if syncErr == nil {
			if current, e := s.Flash(c, user, v.Day); e == nil {
				v = current
			}
		}
		// Unknown delivery is not a successful credit. The saved amount is still
		// displayed and the persistent queue continues independently of this page.
		if syncErr != nil && v.GrantID != "" {
			v.Status = "unavailable"
		}
	}
	summary.CheckinReceipt.FlashAmount = fmt.Sprintf("%d.%02d", v.Cents/100, v.Cents%100)
	summary.CheckinReceipt.FlashStatus = v.Status
	summary.CheckinReceipt.FlashGrantID = v.GrantID
}
