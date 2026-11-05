package pulse_user_center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/communitytickets"
	"github.com/segmentfault/pacman/log"
)

func (uc *UserCenter) communityTicketStore(ctx context.Context) (*communitytickets.Store, error) {
	uc.ticketMu.Lock()
	defer uc.ticketMu.Unlock()
	if uc.ticketStore != nil {
		return uc.ticketStore, nil
	}
	g, err := uc.experienceStore(ctx)
	if err != nil {
		return nil, err
	}
	s := communitytickets.New(g.DB)
	if err = s.Ensure(ctx); err != nil {
		return nil, err
	}
	uc.ticketStore = s
	return s, nil
}

// Started by the authenticated router hook, independently of user visits. The
// database cursor/outbox survive process restarts, midnight and Pulse outages.
func (uc *UserCenter) startCommunityTickets() {
	if os.Getenv("FORUM_BINDING_GUARD_DSN") == "" {
		return
	}
	uc.ticketLoopOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				err := uc.communityTicketBatch(ctx)
				cancel()
				if err != nil {
					log.Warn("community ticket synchronization deferred; durable cursor and receipts retained")
				}
				<-ticker.C
			}
		}()
	})
}
func (uc *UserCenter) communityTicketBatch(ctx context.Context) error {
	s, err := uc.communityTicketStore(ctx)
	if err != nil {
		return err
	}
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	reader, ok := uc.Guard.(communityIdentityReader)
	if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.CommunityBFFHMACSecret) {
		return errors.New("ticket binding unavailable")
	}
	if err = uc.Guard.Ready(ctx); err != nil {
		return err
	}
	// Collection and delivery fail independently: an unavailable Pulse cannot
	// stop local qualification, and a collector error cannot lose saved receipts.
	collectErr := s.Collect(ctx)
	pending, err := s.Pending(ctx)
	if err != nil {
		return err
	}
	for _, receipt := range pending {
		if err = s.Defer(ctx, receipt.ID); err != nil {
			return err
		}
		external, e := reader.CommunityIdentity(ctx, receipt.UserID)
		if e != nil || external != receipt.ExternalID {
			continue
		}
		body, _ := json.Marshal(receipt)
		data, status, e := uc.Client.communityRequest(ctx, http.MethodPost, "/v1/internal/me/community-tickets", external, "", body)
		if e != nil || status != 200 {
			continue
		}
		var result struct {
			EntryID uint64 `json:"entry_id"`
		}
		if decodeCommunityResponse(data, &result) != nil || result.EntryID == 0 {
			continue
		}
		if e = s.Ack(ctx, receipt.ID); e != nil {
			return e
		}
	}
	return collectErr
}
