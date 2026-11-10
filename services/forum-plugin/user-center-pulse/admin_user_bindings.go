package pulse_user_center

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// maxAdminBindingLookup matches the largest /admin/users page size.
const maxAdminBindingLookup = 100

type adminBindingInfo struct {
	APIUserID string   `json:"api_user_id,omitempty"`
	Providers []string `json:"providers,omitempty"`
}

type adminBindingReader interface {
	CheckAdminIdentity(context.Context, string) error
	adminUserBindings(context.Context, []string) (map[string]adminBindingInfo, error)
}

// registerAdminUserBindings serves the read-only binding column of /admin/users.
// It is a POST only so the browser must send Origin like the other admin calls.
func (uc *UserCenter) registerAdminUserBindings(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	if r == nil {
		return
	}
	r.POST("/metar/accounts/bindings", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !ticketGrantEnabled() || c.Request.URL.RawQuery != "" || switchToken(c) == "" || !communitySameOrigin(c.Request, siteURL()) {
			communityError(c, http.StatusForbidden, "forbidden")
			return
		}
		admin, err := session(c)
		if err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		var body struct {
			UserIDs []string `json:"user_ids"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF || len(body.UserIDs) == 0 || len(body.UserIDs) > maxAdminBindingLookup {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		seen := make(map[string]bool, len(body.UserIDs))
		ids := make([]string, 0, len(body.UserIDs))
		for _, id := range body.UserIDs {
			if !canonicalCommunityID(id) {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		guard, ok := uc.Guard.(adminBindingReader)
		if !ok {
			communityError(c, http.StatusServiceUnavailable, "bindings_unavailable")
			return
		}
		if err = guard.CheckAdminIdentity(ctx, admin); err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		result, err := guard.adminUserBindings(ctx, ids)
		if err != nil {
			communityError(c, http.StatusServiceUnavailable, "bindings_unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": result})
	})
}

// adminUserBindings reports the protected new-api binding and other external
// login providers per Answer user. Admin-switch audit rows are not logins.
func (g *mysqlBindingGuard) adminUserBindings(ctx context.Context, ids []string) (map[string]adminBindingInfo, error) {
	args := make([]any, 0, len(ids)+1)
	args = append(args, switchSlug)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := g.db.QueryContext(ctx, "SELECT user_id, provider, external_id FROM user_external_login WHERE provider <> ? AND user_id IN (?"+strings.Repeat(",?", len(ids)-1)+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]adminBindingInfo)
	for rows.Next() {
		var user int64
		var provider, external string
		if err = rows.Scan(&user, &provider, &external); err != nil {
			return nil, err
		}
		key := strconv.FormatInt(user, 10)
		info := result[key]
		if provider == pluginSlug {
			// The guard keeps this one-to-one; never surface a malformed id.
			if canonicalCommunityID(external) {
				info.APIUserID = external
			}
		} else if len(provider) <= 64 {
			info.Providers = append(info.Providers, provider)
		}
		result[key] = info
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for key, info := range result {
		sort.Strings(info.Providers)
		result[key] = info
	}
	return result, nil
}
