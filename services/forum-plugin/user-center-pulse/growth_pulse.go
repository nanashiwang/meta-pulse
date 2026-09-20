package pulse_user_center

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/growth"
	"net/http"
	"time"
)

// No amount or external identity is accepted from the browser. The optional
// protected binding is rechecked before retrieving signed server deliveries.
func (uc *UserCenter) syncPulseExperience(ctx context.Context, forumID string) error {
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	reader, ok := uc.Guard.(communityIdentityReader)
	if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.CommunityBFFHMACSecret) {
		return nil
	}
	external, err := reader.CommunityIdentity(ctx, forumID)
	if errors.Is(err, errCommunityBindingRequired) {
		return nil
	}
	if err != nil {
		return err
	}
	return uc.consumePulseExperience(ctx, forumID, external)
}
func (uc *UserCenter) consumePulseExperience(ctx context.Context, forumID, external string) error {
	store, err := uc.experienceStore(ctx)
	if err != nil {
		return err
	}
	// Bound each refresh. Remaining outbox rows stay pending and cannot be lost,
	// including grants beyond the ordinary 20-item reward history window.
	for page := 0; page < 3; page++ {
		data, status, err := uc.Client.communityRequest(ctx, http.MethodGet, "/v1/internal/me/experience", external, "", nil)
		if err != nil {
			return err
		}
		if status == 404 {
			return nil
		}
		if status != 200 {
			return errors.New("experience delivery unavailable")
		}
		var response struct {
			Deliveries []struct {
				GrantID string `json:"grant_id"`
				Amount  int64  `json:"amount"`
				Status  string `json:"status"`
			} `json:"deliveries"`
		}
		if decodeCommunityResponse(data, &response) != nil || response.Deliveries == nil || len(response.Deliveries) > 20 {
			return errors.New("invalid experience delivery")
		}
		if len(response.Deliveries) == 0 {
			return nil
		}
		for _, d := range response.Deliveries {
			if (d.Status != "pending" && d.Status != "reversed") || d.Amount < 1 || d.Amount > 1000000 {
				return errors.New("invalid experience delivery")
			}
			if err = store.AwardPulse(ctx, forumID, d.GrantID, d.Amount, d.Status == "reversed"); err != nil {
				return err
			}
			body, _ := json.Marshal(map[string]string{"grant_id": d.GrantID, "status": d.Status})
			_, status, err = uc.Client.communityRequest(ctx, http.MethodPost, "/v1/internal/me/experience/ack", external, d.GrantID, body)
			if err != nil {
				return err
			}
			if status != 200 {
				return errors.New("experience acknowledgement pending")
			}
		}
	}
	return errors.New("experience deliveries remain pending")
}
func (uc *UserCenter) reversePulseExperience(ctx context.Context, actor, key, grant, reason string) error {
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	if uc.Client == nil {
		return errors.New("pulse unavailable")
	}
	body, _ := json.Marshal(map[string]string{"grant_id": grant, "reason": reason})
	_, status, err := uc.Client.adminSettingsRequest(ctx, http.MethodPost, "experience-reverse", actor, key, body)
	if err != nil {
		return err
	}
	switch status {
	case 200:
		return nil
	case 400:
		return growth.ErrInvalid
	case 404, 409:
		return growth.ErrConflict
	default:
		return errors.New("pulse unavailable")
	}
}
func (uc *UserCenter) trySyncPulseExperience(ctx context.Context, user string) bool {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return uc.syncPulseExperience(c, user) != nil
}
