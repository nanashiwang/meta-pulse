package pulse_user_center

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (uc *UserCenter) adminRewardFeedHandler(session func(*gin.Context) (string, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		if c.Request.URL.RawQuery != "" || c.GetHeader("X-Metar-Request") != "1" || c.GetHeader("Authorization") == "" || len(c.Request.Header.Values("Authorization")) != 1 || len(c.Request.Header.Values("X-Metar-Request")) != 1 {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		adminID, err := session(c)
		if err != nil {
			adminSettingsIdentityError(c, err)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		identity, ok := uc.Guard.(adminIdentityReader)
		if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.AdminHMACSecret) {
			communityError(c, 503, "winners_unavailable")
			return
		}
		if err := identity.CheckAdminIdentity(ctx, adminID); err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		data, status, err := uc.Client.adminSettingsRequest(ctx, http.MethodGet, "reward-feed", adminID, "", nil)
		if err != nil || status >= 500 {
			communityError(c, 503, "winners_unavailable")
			return
		}
		if status != http.StatusOK {
			adminSettingsUpstreamError(c, status, data)
			return
		}
		if _, err := communityRewardFeedProjection(data); err != nil {
			communityError(c, 503, "winners_unavailable")
			return
		}
		var raw struct {
			Rewards []struct {
				UserID     uint64    `json:"user_id"`
				RewardType string    `json:"reward_type"`
				Amount     int64     `json:"amount"`
				WonAt      time.Time `json:"won_at"`
			} `json:"rewards"`
			QuotaPerUnit int64 `json:"quota_per_unit"`
		}
		if decodeCommunityResponse(data, &raw) != nil || len(raw.Rewards) > 100 || raw.Rewards == nil {
			communityError(c, 503, "winners_unavailable")
			return
		}
		ids := make([]uint64, 0, len(raw.Rewards))
		for _, row := range raw.Rewards {
			if row.UserID == 0 {
				communityError(c, 503, "winners_unavailable")
				return
			}
			ids = append(ids, row.UserID)
		}
		users, ok := uc.Guard.(communityUsernameReader)
		if !ok {
			communityError(c, 503, "winners_unavailable")
			return
		}
		names, err := users.CommunityUsernames(ctx, ids)
		if err != nil {
			communityError(c, 503, "winners_unavailable")
			return
		}
		result := struct {
			Rewards      []map[string]any `json:"rewards"`
			QuotaPerUnit int64            `json:"quota_per_unit"`
		}{Rewards: make([]map[string]any, 0, len(raw.Rewards)), QuotaPerUnit: raw.QuotaPerUnit}
		for _, row := range raw.Rewards {
			name := names[row.UserID]
			if name == "" {
				name = "匿名用户"
			}
			result.Rewards = append(result.Rewards, map[string]any{"username": name, "reward_type": row.RewardType, "amount": row.Amount, "won_at": row.WonAt.UTC().Truncate(time.Minute)})
		}
		c.JSON(http.StatusOK, result)
	}
}
