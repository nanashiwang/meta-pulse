package pulse_user_center

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// The model leaderboard admin (services/intel) is reached only through this
// BFF: the plugin proves the caller is a current Answer administrator, then
// signs a fixed upstream path with a secret Intel shares with no browser.
// Configuration comes from the forum container's environment so it stays out
// of the Answer plugin settings that other admins can read.

const (
	// Matches nginx's default client_max_body_size on the forum location.
	maxIntelAdminRequest  = 1 << 20
	maxIntelAdminResponse = 16 << 20
	intelAdminTimeout     = 60 * time.Second
)

type intelAdminTarget struct {
	BaseURL string
	Secret  string
}

var intelAdminConfig = func() intelAdminTarget {
	return intelAdminTarget{BaseURL: os.Getenv("METAR_INTEL_ADMIN_URL"), Secret: os.Getenv("METAR_INTEL_ADMIN_HMAC_SECRET")}
}

var intelAdminHTTP = &http.Client{Timeout: intelAdminTimeout}

var intelIdempotencyKey = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type intelAdminOperation struct {
	method, upstream string
	body, idempotent bool
}

var intelAdminOperations = map[string]intelAdminOperation{
	"sources":               {http.MethodGet, "/v1/internal/admin/sources", false, false},
	"sources/llmstats":      {http.MethodPut, "/v1/internal/admin/sources/llmstats", true, false},
	"sources/llmstats/sync": {http.MethodPost, "/v1/internal/admin/sources/llmstats/sync", false, false},
	"overview":              {http.MethodGet, "/v1/internal/admin/overview", false, false},
	"export":                {http.MethodGet, "/v1/internal/admin/export", false, false},
	"validate":              {http.MethodPost, "/v1/internal/admin/validate", true, false},
	"import":                {http.MethodPost, "/v1/internal/admin/import", true, true},
	"recompute":             {http.MethodPost, "/v1/internal/admin/recompute", false, false},
}

func (uc *UserCenter) registerIntelAdmin(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	if r == nil {
		return
	}
	for name, op := range intelAdminOperations {
		r.Handle(op.method, "/metar/intel/"+name, uc.intelAdminHandler(op, session, siteURL))
	}
}

func (uc *UserCenter) intelAdminHandler(op intelAdminOperation, session func(*gin.Context) (string, error), siteURL func() string) gin.HandlerFunc {
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
		if op.method != http.MethodGet {
			if !communitySameOrigin(c.Request, siteURL()) {
				communityError(c, http.StatusForbidden, "forbidden")
				return
			}
			body, err = io.ReadAll(io.LimitReader(c.Request.Body, maxIntelAdminRequest+1))
			if err != nil || len(body) > maxIntelAdminRequest {
				communityError(c, http.StatusRequestEntityTooLarge, "document_too_large")
				return
			}
			if op.body {
				mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
				trimmed := bytes.TrimSpace(body)
				if err != nil || mediaType != "application/json" || len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(body) {
					communityError(c, http.StatusBadRequest, "invalid_request")
					return
				}
			} else if len(bytes.TrimSpace(body)) != 0 {
				communityError(c, http.StatusBadRequest, "invalid_request")
				return
			}
			if op.idempotent {
				key = c.GetHeader("Idempotency-Key")
				if !intelIdempotencyKey.MatchString(key) || len(c.Request.Header.Values("Idempotency-Key")) != 1 {
					communityError(c, http.StatusBadRequest, "invalid_idempotency_key")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), intelAdminTimeout)
		defer cancel()
		uc.runtimeMu.RLock()
		reader, ok := uc.Guard.(adminIdentityReader)
		if !ok {
			uc.runtimeMu.RUnlock()
			communityError(c, http.StatusServiceUnavailable, "intel_unavailable")
			return
		}
		err = reader.CheckAdminIdentity(ctx, adminID)
		uc.runtimeMu.RUnlock()
		if err != nil {
			adminSettingsIdentityError(c, err)
			return
		}
		data, status, err := intelAdminRequest(ctx, intelAdminConfig(), op, adminID, key, body)
		if err != nil || status >= 500 {
			// Distinguish "the key could not be checked with LLM Stats" from
			// Intel being down; nothing else from a 5xx body is forwarded.
			var failure struct {
				Error string `json:"error"`
			}
			if err == nil && status == http.StatusServiceUnavailable && json.Unmarshal(data, &failure) == nil && failure.Error == "verify_unavailable" {
				communityError(c, http.StatusServiceUnavailable, "verify_unavailable")
				return
			}
			communityError(c, http.StatusServiceUnavailable, "intel_unavailable")
			return
		}
		switch status {
		case http.StatusOK, http.StatusAccepted:
			if !json.Valid(data) {
				communityError(c, http.StatusServiceUnavailable, "intel_unavailable")
				return
			}
			writeIntelJSON(c, status, data)
		case http.StatusUnprocessableEntity, http.StatusConflict, http.StatusBadRequest:
			intelAdminUpstreamError(c, status, data)
		default:
			communityError(c, http.StatusServiceUnavailable, "intel_unavailable")
		}
	}
}

// Avoid Gin Data: it sets the original Content-Length after Answer Brotli clears it.
// Let the response writer frame the compressed bytes actually sent.
func writeIntelJSON(c *gin.Context, status int, data []byte) {
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(status)
	if _, err := c.Writer.Write(data); err != nil {
		_ = c.Error(err)
		c.Abort()
	}
}

// intelAdminUpstreamError forwards only known error codes and validation
// problem strings, never arbitrary upstream bodies.
func intelAdminUpstreamError(c *gin.Context, status int, data []byte) {
	var failure struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
	}
	allowed := map[string]int{"invalid_document": 422, "idempotency_conflict": 409, "invalid_idempotency_key": 400, "invalid_request": 400,
		"invalid_api_key": 422, "managed_by_env": 409, "not_configured": 409}
	if json.Unmarshal(data, &failure) != nil || allowed[failure.Error] != status {
		communityError(c, http.StatusServiceUnavailable, "intel_unavailable")
		return
	}
	if len(failure.Problems) > 200 {
		failure.Problems = failure.Problems[:200]
	}
	out := gin.H{"error": failure.Error}
	if failure.Problems != nil {
		out["problems"] = failure.Problems
	}
	c.AbortWithStatusJSON(status, out)
}

func intelAdminRequest(ctx context.Context, target intelAdminTarget, op intelAdminOperation, adminID, key string, body []byte) ([]byte, int, error) {
	base, err := url.Parse(strings.TrimRight(target.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
		len(target.Secret) < 32 || !canonicalCommunityID(adminID) {
		return nil, 0, errIntelAdminUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, op.method, base.String()+op.upstream, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	raw := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return nil, 0, err
	}
	nonce := hex.EncodeToString(raw)
	timestamp := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(target.Secret))
	_, _ = mac.Write([]byte(pulseCanonicalPayload(op.method, req.URL.EscapedPath(), adminID, "admin", timestamp, nonce, body)))
	req.Header.Set("X-Intel-User-Id", adminID)
	req.Header.Set("X-Intel-Role", "admin")
	req.Header.Set("X-Intel-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Intel-Nonce", nonce)
	req.Header.Set("X-Intel-Signature", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Accept", "application/json")
	if op.body {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := intelAdminHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIntelAdminResponse+1))
	if err != nil || len(data) > maxIntelAdminResponse {
		return nil, resp.StatusCode, errIntelAdminUnavailable
	}
	return data, resp.StatusCode, nil
}

var errIntelAdminUnavailable = intelAdminError("intel admin unavailable")

type intelAdminError string

func (e intelAdminError) Error() string { return string(e) }
