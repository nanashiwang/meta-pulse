package pulse_user_center

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const adminSnapshotFixture = `{"revision":1,"environment":"production","config":{"newapi_internal_base_url":"http://newapi:3000","quota_per_unit":"500000","actions_enabled":false,"reward_shadow_mode":true},"secrets":{"PULSE_SERVICE_HMAC_SECRET":{"configured":true,"source":"database","value":"must-not-leak"}},"worker_ready":true,"newapi_target_locked":true,"raw_secret":"must-not-leak"}`
const adminUpdateFixture = `{"revision":1,"config":{"quota_per_unit":"500000"},"secrets":{"PULSE_SERVICE_HMAC_SECRET":"worker-only-submitted-secret-value"},"reason":"更新发奖配置"}`

type adminTestGuard struct {
	fakeBindingGuard
	seen string
}

func (g *adminTestGuard) CheckAdminIdentity(_ context.Context, id string) error {
	g.seen = id
	return g.err
}

func adminTestRouter(t *testing.T, upstream http.HandlerFunc, sessionError error) (*gin.Engine, *UserCenter, *adminTestGuard) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	cfg := validPluginConfig()
	cfg.PulseBaseURL = server.URL
	cfg.AdminHMACSecret = strings.Repeat("a", 32)
	guard := &adminTestGuard{}
	uc := &UserCenter{Config: cfg, Client: NewPulseClient(cfg), Guard: guard}
	router := gin.New()
	uc.registerAdminSettingsRoutes(router.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", sessionError }, func() string { return "https://metar.example.test" })
	return router, uc, guard
}

func adminTestRequest(method, operation, payload string) *http.Request {
	r := communityTestRequest(method, operation, payload)
	r.URL.Path = "/answer/admin/api/metar/pulse/" + strings.Split(operation, "?")[0]
	return r
}

func TestAdminSettingsBFFDerivesAdminAndUsesIndependentSignature(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			operation, body := "settings", ""
			if method == http.MethodPut {
				body = adminUpdateFixture
			} else if method == http.MethodPost {
				operation, body = "secret", `{}`
			}
			router, uc, guard := adminTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				path := "/v1/internal/admin/settings"
				if method == http.MethodPost {
					path += "/secret"
				}
				if r.Method != method || r.URL.Path != path || string(data) != body {
					t.Errorf("incorrect method, path or raw body")
				}
				if r.Header.Get(pulseHeaderUserID) != "7" || r.Header.Get(pulseHeaderRole) != "admin" {
					t.Error("server admin identity not used")
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("New-Api-User") != "" {
					t.Error("browser credentials forwarded")
				}
				if method == http.MethodPut && r.Header.Get("Idempotency-Key") != "request_1" {
					t.Error("missing stable mutation key")
				}
				timestamp, _ := strconv.ParseInt(r.Header.Get(pulseHeaderTimestamp), 10, 64)
				mac := hmac.New(sha256.New, []byte(strings.Repeat("a", 32)))
				_, _ = mac.Write([]byte(pulseCanonicalPayload(method, path, "7", "admin", timestamp, r.Header.Get(pulseHeaderNonce), data)))
				if r.Header.Get(pulseHeaderSignature) != hex.EncodeToString(mac.Sum(nil)) {
					t.Error("admin signature mismatch")
				}
				if method == http.MethodPost {
					_ = json.NewEncoder(w).Encode(map[string]string{"secret": strings.Repeat("a1", 32), "raw_secret": "must-not-leak"})
				} else {
					_, _ = w.Write([]byte(adminSnapshotFixture))
				}
			}, nil)
			r := adminTestRequest(method, operation, body)
			r.Header.Set(pulseHeaderUserID, "999")
			r.Header.Set(pulseHeaderRole, "community-bff")
			r.Header.Set("Cookie", "visit=private-session")
			r.Header.Set("New-Api-User", "999")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if method != http.MethodPost && !strings.Contains(w.Body.String(), `"newapi_target_locked":true`) {
				t.Error("receiver target lock was lost")
			}
			if w.Code != http.StatusOK || guard.seen != "7" || strings.Contains(w.Body.String(), "must-not-leak") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("bad status, identity, privacy projection or cache policy: %d %s", w.Code, w.Body.String())
			}
			cfg, _ := json.Marshal(uc.configSnapshot())
			if strings.Contains(string(cfg), "worker-only-submitted-secret-value") {
				t.Error("plugin retained worker settlement secret")
			}
		})
	}
}

func TestAdminSettingsRejectsMalformedRequestsBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, method, operation, body string
		mutate                        func(*http.Request)
		status                        int
	}{
		{"URL authorization", "GET", "settings?Authorization=token", "", nil, 400},
		{"foreign user query", "GET", "settings?user_id=9", "", nil, 400},
		{"missing auth", "GET", "settings", "", func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"duplicate auth", "GET", "settings", "", func(r *http.Request) { r.Header.Add("Authorization", "other") }, 401},
		{"missing marker", "GET", "settings", "", func(r *http.Request) { r.Header.Del("X-Metar-Request") }, 400},
		{"cross origin", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Set("Origin", "https://attacker.test") }, 403},
		{"missing origin", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"cross site metadata", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"form body", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") }, 400},
		{"missing key", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Del("Idempotency-Key") }, 400},
		{"duplicate key", "PUT", "settings", adminUpdateFixture, func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") }, 400},
		{"duplicate revision", "PUT", "settings", `{"revision":1,"revision":2,"reason":"update"}`, nil, 400},
		{"duplicate config field", "PUT", "settings", `{"revision":1,"reason":"update","config":{"actions_enabled":true,"actions_enabled":false}}`, nil, 400},
		{"duplicate secret field", "PUT", "settings", `{"revision":1,"reason":"update","secrets":{"PULSE_ADMIN_HMAC_SECRET":"a","PULSE_ADMIN_HMAC_SECRET":"b"}}`, nil, 400},
		{"null config", "PUT", "settings", `{"revision":1,"reason":"update","config":null}`, nil, 400},
		{"null config field", "PUT", "settings", `{"revision":1,"reason":"update","config":{"actions_enabled":null}}`, nil, 400},
		{"unknown secret", "PUT", "settings", `{"revision":1,"reason":"update","secrets":{"PULSE_REWARD_RANDOM_SECRET":"a"}}`, nil, 400},
		{"actor injection", "PUT", "settings", `{"revision":1,"reason":"update","actor_id":"999"}`, nil, 400},
		{"trailing body", "PUT", "settings", adminUpdateFixture + `{}`, nil, 400},
		{"large body", "PUT", "settings", strings.Repeat(" ", maxAdminSettingsBytes) + adminUpdateFixture, nil, 400},
		{"secret arguments", "POST", "secret", `{"length":64}`, nil, 400},
		{"secret null", "POST", "secret", `null`, nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			router, _, _ := adminTestRouter(t, func(http.ResponseWriter, *http.Request) { called = true }, nil)
			r := adminTestRequest(tc.method, tc.operation, tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if called || w.Code != tc.status {
				t.Fatalf("called=%v status=%d body=%s", called, w.Code, w.Body.String())
			}
		})
	}
}

func TestAdminSettingsRejectsCachedOrLiveUnavailableIdentity(t *testing.T) {
	for _, err := range []error{errAdminForbidden, errCommunityAccountUnavailable, errCommunityUnauthenticated, errors.New("private DB error must-not-leak")} {
		for _, live := range []bool{true, false} {
			called := false
			var sessionError error
			if !live {
				sessionError = err
			}
			router, _, guard := adminTestRouter(t, func(http.ResponseWriter, *http.Request) { called = true }, sessionError)
			if live {
				guard.err = err
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, adminTestRequest("GET", "settings", ""))
			if called || w.Code == http.StatusOK || strings.Contains(w.Body.String(), "must-not-leak") {
				t.Fatalf("identity failure was not closed safely: %d %s", w.Code, w.Body.String())
			}
		}
	}
}

func TestAdminSettingsErrorsAndResponseProjectionNeverLeakSecrets(t *testing.T) {
	for _, raw := range []string{`null`, adminSnapshotFixture + `{}`, strings.Replace(adminSnapshotFixture, `"source":"database"`, `"source":"must-not-leak"`, 1)} {
		if _, err := adminSettingsProjection("settings", []byte(raw)); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	for _, status := range []int{400, 401, 403, 409, 500} {
		router, _, _ := adminTestRouter(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"must-not-leak","message":"raw-secret"}`))
		}, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, adminTestRequest("GET", "settings", ""))
		if w.Code != 503 || strings.Contains(w.Body.String(), "must-not-leak") || strings.Contains(w.Body.String(), "raw-secret") {
			t.Fatalf("upstream diagnostic leaked: %d %s", w.Code, w.Body.String())
		}
	}
	router, _, _ := adminTestRouter(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"settings_conflict","message":"raw-secret"}`))
	}, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, adminTestRequest("PUT", "settings", adminUpdateFixture))
	if w.Code != 409 || w.Body.String() != `{"error":"settings_conflict"}` {
		t.Fatal("safe conflict code lost")
	}
}

func TestAdminSettingsMissingPairingDoesNotAffectLocalLogin(t *testing.T) {
	router, uc, _ := adminTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Error("unpaired upstream called") }, nil)
	uc.Config.AdminHMACSecret = ""
	w := httptest.NewRecorder()
	router.ServeHTTP(w, adminTestRequest("GET", "settings", ""))
	if w.Code != 503 || !uc.Description().EnabledOriginalUserSystem {
		t.Fatal("optional admin pairing changed local login")
	}
}

func TestAdminSessionRejectsBrowserAndLookalikeIdentity(t *testing.T) {
	type cache struct {
		UserID                          string
		RoleID, UserStatus, EmailStatus int
	}
	for _, value := range []any{nil, "7", map[string]any{"UserID": "7", "RoleID": 2}, &cache{"7", 2, 1, 1}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("ctxUuidKey", value)
		if _, err := answerAdminSessionUserID(c); err == nil {
			t.Fatal("fake admin session accepted")
		}
	}
}
