package pulse_user_center

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (uc *UserCenter) giftSpend(ctx context.Context, forumUser, external, day string) (*int64, error) {
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	if uc.Client == nil {
		return nil, errors.New("usage unavailable")
	}
	reader, ok := uc.Guard.(communityIdentityReader)
	if !ok {
		return nil, errors.New("binding unavailable")
	}
	bound, e := reader.CommunityIdentity(ctx, forumUser)
	if e != nil || bound != external {
		return nil, errors.New("binding changed")
	}
	data, status, e := uc.Client.communityRequest(ctx, http.MethodGet, "/v1/internal/me/gift-usage", external, "", nil)
	if e != nil || status != 200 {
		return nil, errors.New("usage temporarily unavailable")
	}
	var v struct {
		Day   string
		Quota json.RawMessage
	}
	if decodeCommunityResponse(data, &v) != nil || v.Day != day || len(v.Quota) == 0 {
		return nil, errors.New("usage date unavailable")
	}
	var q *int64
	if json.Unmarshal(v.Quota, &q) != nil || q != nil && *q < 0 {
		return nil, errors.New("usage invalid")
	}
	return q, nil
}
func (uc *UserCenter) giftMember(session func(*gin.Context) (string, error), site func() string, claim bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if c.GetHeader("Authorization") == "" || c.Request.URL.RawQuery != "" {
			communityError(c, 401, "authentication_required")
			return
		}
		user, e := session(c)
		if e != nil {
			communityIdentityError(c, e)
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		if claim && (!communitySameOrigin(c.Request, site()) || c.GetHeader("X-Metar-Request") != "1" || !chatJSON(c, &body) || !canonicalCommunityID(body.ID)) {
			communityError(c, 400, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		s, e := uc.communityEventStore(ctx)
		if e != nil {
			communityError(c, 503, "unavailable")
			return
		}
		offer, receipt, e := s.Gift(ctx, user, body.ID, func(c context.Context, ext, day string) (*int64, error) { return uc.giftSpend(c, user, ext, day) })
		if e != nil {
			communityError(c, 503, "gift_unavailable")
			return
		}
		if claim && receipt == nil {
			communityError(c, 409, "gift_closed")
			return
		}
		c.JSON(200, gin.H{"code": 200, "data": gin.H{"offer": offer, "receipt": receipt}})
	}
}
