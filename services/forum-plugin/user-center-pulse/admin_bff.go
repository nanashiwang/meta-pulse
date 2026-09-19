package pulse_user_center

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const maxAdminSettingsBytes = 64 << 10

var adminSettingsSecretNames = map[string]bool{
	"PULSE_FORUM_HMAC_SECRET": true, "PULSE_FORUM_HMAC_SECRET_PREVIOUS": true,
	"PULSE_USER_BFF_HMAC_SECRET": true, "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS": true,
	"PULSE_COMMUNITY_BFF_HMAC_SECRET": true, "PULSE_COMMUNITY_BFF_HMAC_SECRET_PREVIOUS": true,
	"PULSE_ADMIN_HMAC_SECRET": true, "PULSE_ADMIN_HMAC_SECRET_PREVIOUS": true,
	"PULSE_ROLLBACK_HMAC_SECRET": true, "PULSE_ROLLBACK_HMAC_SECRET_PREVIOUS": true,
	"PULSE_SERVICE_HMAC_SECRET": true, "PULSE_SERVICE_HMAC_SECRET_PREVIOUS": true,
}

func (uc *UserCenter) registerAdminSettingsRoutes(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	if r == nil {
		return
	}
	r.GET("/metar/pulse/settings", uc.adminSettingsHandler("settings", session, siteURL))
	r.PUT("/metar/pulse/settings", uc.adminSettingsHandler("settings", session, siteURL))
	r.POST("/metar/pulse/secret", uc.adminSettingsHandler("secret", session, siteURL))
}

func (uc *UserCenter) adminSettingsHandler(operation string, session func(*gin.Context) (string, error), siteURL func() string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		if c.Request.URL.RawQuery != "" || c.GetHeader("X-Metar-Request") != "1" || len(c.Request.Header.Values("X-Metar-Request")) != 1 {
			communityError(c, http.StatusBadRequest, "invalid_request")
			return
		}
		if c.GetHeader("Authorization") == "" || len(c.Request.Header.Values("Authorization")) != 1 {
			communityError(c, http.StatusUnauthorized, "authentication_required")
			return
		}
		adminID, err := session(c)
		if err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		var body []byte
		key := ""
		if c.Request.Method != http.MethodGet {
			if !communitySameOrigin(c.Request, siteURL()) {
				communityError(c, http.StatusForbidden, "forbidden")
				return
			}
			mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || mediaType != "application/json" {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, maxAdminSettingsBytes+1))
			if err != nil || len(body) > maxAdminSettingsBytes || !validAdminSettingsBody(operation, body) {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			if c.Request.Method == http.MethodPut {
				key = c.GetHeader("Idempotency-Key")
				if !communityRequestID.MatchString(key) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
					communityError(c, http.StatusBadRequest, "invalid_idempotency_key")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		reader, ok := uc.Guard.(adminIdentityReader)
		if !ok || uc.Client == nil || uc.Config == nil || !usableConfigSecret(uc.Config.AdminHMACSecret) {
			communityError(c, http.StatusServiceUnavailable, "settings_unavailable")
			return
		}
		if err := reader.CheckAdminIdentity(ctx, adminID); err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		data, status, err := uc.Client.adminSettingsRequest(ctx, c.Request.Method, operation, adminID, key, body)
		if err != nil || status >= 500 {
			communityError(c, http.StatusServiceUnavailable, "settings_unavailable")
			return
		}
		if status != http.StatusOK {
			adminSettingsUpstreamError(c, status, data)
			return
		}
		result, err := adminSettingsProjection(operation, data)
		if err != nil {
			communityError(c, http.StatusServiceUnavailable, "settings_unavailable")
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func adminSettingsIdentityError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errCommunityUnauthenticated):
		communityError(c, http.StatusUnauthorized, "authentication_required")
	case errors.Is(err, errAdminForbidden), errors.Is(err, errCommunityAccountUnavailable):
		communityError(c, http.StatusForbidden, "forbidden")
	default:
		communityError(c, http.StatusServiceUnavailable, "settings_unavailable")
	}
}

func adminSettingsUpstreamError(c *gin.Context, status int, data []byte) {
	var failure struct {
		Error string `json:"error"`
	}
	if decodeCommunityResponse(data, &failure) == nil {
		allowed := map[string]int{"invalid_settings": 400, "settings_conflict": 409, "forbidden": 403, "invalid_idempotency_key": 400, "invalid_request": 400}
		if allowed[failure.Error] == status {
			communityError(c, status, failure.Error)
			return
		}
	}
	communityError(c, http.StatusServiceUnavailable, "settings_unavailable")
}

// Strict object decoding catches duplicate keys before any stateful upstream
// request. No raw body, submitted key or upstream diagnostic is ever logged.
func adminJSONObject(data []byte) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(data))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, false
	}
	result := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, exists := result[key]; exists {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
		result[key] = value
	}
	end, err := d.Token()
	var trailing any
	if err != nil || end != json.Delim('}') || !errors.Is(d.Decode(&trailing), io.EOF) {
		return nil, false
	}
	return result, true
}

func validAdminSettingsBody(operation string, body []byte) bool {
	fields, ok := adminJSONObject(body)
	if !ok {
		return false
	}
	if operation == "secret" {
		return len(fields) == 0
	}
	var revision uint64
	var reason string
	if json.Unmarshal(fields["revision"], &revision) != nil || json.Unmarshal(fields["reason"], &reason) != nil || strings.TrimSpace(reason) == "" || len(reason) > 1000 {
		return false
	}
	for name, raw := range fields {
		switch name {
		case "revision", "reason":
		case "config":
			config, ok := adminJSONObject(raw)
			if !ok {
				return false
			}
			for field, value := range config {
				switch field {
				case "newapi_internal_base_url", "quota_per_unit":
					var s string
					if json.Unmarshal(value, &s) != nil {
						return false
					}
				case "actions_enabled", "reward_shadow_mode":
					var b bool
					if json.Unmarshal(value, &b) != nil {
						return false
					}
				default:
					return false
				}
			}
		case "secrets":
			secrets, ok := adminJSONObject(raw)
			if !ok {
				return false
			}
			for name, value := range secrets {
				var secret string
				if !adminSettingsSecretNames[name] || json.Unmarshal(value, &secret) != nil || len(secret) > 4096 {
					return false
				}
			}
		case "clear_secrets":
			var names []string
			if json.Unmarshal(raw, &names) != nil {
				return false
			}
			seen := map[string]bool{}
			for _, name := range names {
				if !adminSettingsSecretNames[name] || seen[name] {
					return false
				}
				seen[name] = true
			}
		default:
			return false
		}
	}
	return true
}

type adminSettingsConfig struct {
	NewAPIInternalBaseURL string `json:"newapi_internal_base_url"`
	QuotaPerUnit          string `json:"quota_per_unit"`
	ActionsEnabled        bool   `json:"actions_enabled"`
	RewardShadowMode      bool   `json:"reward_shadow_mode"`
}

type adminSettingsSecretStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
}

type adminSettingsSnapshot struct {
	Revision           uint64                               `json:"revision"`
	Config             *adminSettingsConfig                 `json:"config"`
	Secrets            map[string]adminSettingsSecretStatus `json:"secrets"`
	WorkerReady        bool                                 `json:"worker_ready"`
	NewAPITargetLocked bool                                 `json:"newapi_target_locked"`
	Environment        string                               `json:"environment,omitempty"`
}

func adminSettingsProjection(operation string, data []byte) (any, error) {
	invalid := errors.New("invalid admin settings response")
	if operation == "secret" {
		var result struct {
			Secret string `json:"secret"`
		}
		if decodeCommunityResponse(data, &result) != nil || len(result.Secret) != 64 {
			return nil, invalid
		}
		decoded, err := hex.DecodeString(result.Secret)
		if err != nil || hex.EncodeToString(decoded) != result.Secret {
			return nil, invalid
		}
		return result, nil
	}
	var result adminSettingsSnapshot
	if decodeCommunityResponse(data, &result) != nil || result.Config == nil || result.Secrets == nil {
		return nil, invalid
	}
	fields, ok := adminJSONObject(data)
	var locked bool
	if !ok || json.Unmarshal(fields["newapi_target_locked"], &locked) != nil {
		return nil, invalid
	}
	if result.Environment != "" && result.Environment != "development" && result.Environment != "production" {
		return nil, invalid
	}
	q, err := strconv.ParseInt(result.Config.QuotaPerUnit, 10, 64)
	if err != nil || q < 0 || strconv.FormatInt(q, 10) != result.Config.QuotaPerUnit {
		return nil, invalid
	}
	u, err := url.Parse(result.Config.NewAPIInternalBaseURL)
	if err != nil || (result.Config.NewAPIInternalBaseURL != "" && ((u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/"))) {
		return nil, invalid
	}
	for name, status := range result.Secrets {
		if !adminSettingsSecretNames[name] {
			delete(result.Secrets, name)
			continue
		}
		if status.Source != "environment" && status.Source != "database" && status.Source != "unset" {
			return nil, invalid
		}
	}
	return result, nil
}
