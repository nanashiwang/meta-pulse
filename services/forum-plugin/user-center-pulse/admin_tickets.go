package pulse_user_center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
)

const maxAdminTicketGrant = 100

// ticketGrantEnabled is replaceable in tests; production reads Answer's plugin switch.
var ticketGrantEnabled = func() bool { return plugin.StatusManager.IsEnabled(pluginSlug) }

type adminTicketTargetReader interface {
	CheckAdminIdentity(context.Context, string) error
	adminTicketTarget(context.Context, string) (string, error)
}

type adminTicketGrantBody struct {
	Username string `json:"username"`
	Count    int64  `json:"count"`
	Reason   string `json:"reason"`
}

// registerAdminTicketGrant serves the /admin/users row action. The browser names
// only an Answer username; the live binding decides the new-api/Pulse user.
func (uc *UserCenter) registerAdminTicketGrant(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	if r == nil {
		return
	}
	r.POST("/metar/accounts/tickets", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !ticketGrantEnabled() || c.Request.URL.RawQuery != "" || switchToken(c) == "" || c.GetHeader("X-Metar-Request") != "1" || !communitySameOrigin(c.Request, siteURL()) {
			communityError(c, http.StatusForbidden, "forbidden")
			return
		}
		admin, err := session(c)
		if err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		key := c.GetHeader("Idempotency-Key")
		var body adminTicketGrantBody
		reason := ""
		if len(c.Request.Header.Values("Idempotency-Key")) != 1 || !communityRequestID.MatchString(key) || len(key) > 64 || !experienceJSON(c, &body) {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		reason = strings.TrimSpace(body.Reason)
		if body.Username == "" || len(body.Username) > 100 || body.Count < 1 || body.Count > maxAdminTicketGrant || utf8.RuneCountInString(reason) < 2 || utf8.RuneCountInString(reason) > 200 {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		guard, ok := uc.Guard.(adminTicketTargetReader)
		if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.AdminHMACSecret) {
			communityError(c, http.StatusServiceUnavailable, "ticket_grant_unavailable")
			return
		}
		if err = guard.CheckAdminIdentity(ctx, admin); err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		pulseUser, err := guard.adminTicketTarget(ctx, body.Username)
		switch {
		case errors.Is(err, errCommunityBindingRequired):
			communityError(c, http.StatusConflict, "binding_required")
			return
		case errors.Is(err, errCommunityAccountUnavailable):
			communityError(c, http.StatusNotFound, "account_unavailable")
			return
		case err != nil:
			communityError(c, http.StatusServiceUnavailable, "ticket_grant_unavailable")
			return
		}
		user, _ := strconv.ParseUint(pulseUser, 10, 64)
		payload, _ := json.Marshal(struct {
			UserID uint64 `json:"user_id"`
			Count  int64  `json:"count"`
			Reason string `json:"reason"`
		}{user, body.Count, reason})
		// Scope the browser retry key by administrator so two admins cannot collide.
		data, status, err := uc.Client.adminSettingsRequest(ctx, http.MethodPost, "ticket-grant", admin, "a"+admin+"-"+key, payload)
		if err != nil || status >= 500 {
			communityError(c, http.StatusServiceUnavailable, "ticket_grant_unavailable")
			return
		}
		if status == http.StatusConflict {
			communityError(c, http.StatusConflict, "ticket_grant_conflict")
			return
		}
		var result struct {
			Count          int64     `json:"count"`
			QuotaExpiresAt time.Time `json:"quota_expires_at"`
		}
		if status != http.StatusOK || decodeCommunityResponse(data, &result) != nil || result.Count != body.Count || result.QuotaExpiresAt.IsZero() {
			communityError(c, http.StatusServiceUnavailable, "ticket_grant_unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": result})
	})
}

// adminTicketTarget resolves an Answer username to its bound new-api user id,
// rechecking live account status and the immutable one-to-one binding.
func (g *mysqlBindingGuard) adminTicketTarget(ctx context.Context, username string) (string, error) {
	rows, err := g.db.QueryContext(ctx, "SELECT id FROM `user` WHERE username=? LIMIT 2", username)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", errCommunityAccountUnavailable
	}
	id := ids[0]
	return g.CommunityIdentity(ctx, strconv.FormatInt(id, 10))
}
