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

type communityTestGuard struct {
	fakeBindingGuard
	identity string
	seen     string
}

func (g *communityTestGuard) CommunityIdentity(_ context.Context, id string) (string, error) {
	g.seen = id
	return g.identity, g.err
}

func communityTestRouter(t *testing.T, upstream http.HandlerFunc) (*gin.Engine, *UserCenter, *communityTestGuard) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	cfg := validPluginConfig()
	cfg.PulseBaseURL = server.URL
	cfg.CommunityBFFHMACSecret = strings.Repeat("c", 32)
	guard := &communityTestGuard{identity: "42"}
	uc := &UserCenter{Config: cfg, Client: NewPulseClient(cfg), Guard: guard}
	router := gin.New()
	uc.registerCommunityRoutes(router.Group("/answer/api/v1"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	return router, uc, guard
}

func communityTestRequest(method, operation, payload string) *http.Request {
	r := httptest.NewRequest(method, "/answer/api/v1/metar/pulse/"+operation, strings.NewReader(payload))
	r.Header.Set("Authorization", "Bearer answer-session-secret")
	r.Header.Set("Origin", "https://metar.example.test")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Metar-Request", "1")
	r.Header.Set("Idempotency-Key", "request_1")
	return r
}

func TestCommunityBFFDerivesIdentityAndUsesIndependentSignature(t *testing.T) {
	router, _, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/internal/me/actions" || string(body) != `{"action_id":"action_1","trigger_type":"pulse"}` {
			t.Errorf("path/body = %s %s", r.URL.Path, body)
		}
		if r.Header.Get(pulseHeaderUserID) != "42" || r.Header.Get(pulseHeaderRole) != "community-bff" || r.Header.Get("Idempotency-Key") != "request_1" {
			t.Errorf("incorrect identity/role/idempotency headers")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials leaked to Pulse")
		}
		timestamp, _ := strconv.ParseInt(r.Header.Get(pulseHeaderTimestamp), 10, 64)
		mac := hmac.New(sha256.New, []byte(strings.Repeat("c", 32)))
		_, _ = mac.Write([]byte(pulseCanonicalPayload(r.Method, r.URL.EscapedPath(), "42", communityServiceRole, timestamp, r.Header.Get(pulseHeaderNonce), body)))
		if hex.EncodeToString(mac.Sum(nil)) != r.Header.Get(pulseHeaderSignature) {
			t.Error("invalid independent community signature")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"grant_id":"pg_1","period_id":9,"action_id":"action_1","reward_type":"quota","amount":100,"status":"pending","random_value":"secret-random","user_id":42,"source_ref":"private"}`))
	})
	req := communityTestRequest(http.MethodPost, "actions", `{"action_id":"action_1"}`)
	req.Header.Set(pulseHeaderUserID, "999")
	req.Header.Set(pulseHeaderSignature, "forged")
	req.Header.Set("Cookie", "session=must-not-forward")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated || guard.seen != "7" {
		t.Fatalf("status=%d guard=%s body=%s", w.Code, guard.seen, w.Body.String())
	}
	for _, private := range []string{"random_value", "user_id", "source_ref", "secret-random"} {
		if strings.Contains(w.Body.String(), private) {
			t.Errorf("private field leaked: %s", private)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("missing no-store")
	}
}

func TestCommunityBFFRejectsBrowserIdentityAndCSRFBeforeUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, operation, body string
		mutate                func(*http.Request)
		status                int
	}{
		{"user identity", "actions", `{"action_id":"a","user_id":999}`, nil, 400},
		{"trigger selection", "actions", `{"action_id":"a","trigger_type":"content"}`, nil, 400},
		{"duplicate action", "actions", `{"action_id":"a","action_id":"b"}`, nil, 400},
		{"trailing JSON", "actions", `{"action_id":"a"}{}`, nil, 400},
		{"null", "actions", `null`, nil, 400},
		{"array", "actions", `[]`, nil, 400},
		{"blank action", "actions", `{"action_id":""}`, nil, 400},
		{"oversized", "actions", `{"action_id":"` + strings.Repeat("a", 4096) + `"}`, nil, 400},
		{"foreign origin", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example.test") }, 403},
		{"missing origin", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"null origin", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Set("Origin", "null") }, 403},
		{"forged host", "actions", `{"action_id":"a"}`, func(r *http.Request) {
			r.Host = "evil.example.test"
			r.Header.Set("Origin", "https://evil.example.test")
		}, 403},
		{"missing custom header", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Del("X-Metar-Request") }, 403},
		{"cross-site fetch", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"form post", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 400},
		{"missing key", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Del("Idempotency-Key") }, 400},
		{"duplicate key", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Add("Idempotency-Key", "request_2") }, 400},
		{"missing auth header", "actions", `{"action_id":"a"}`, func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"query identity", "summary?user_id=999", ``, nil, 400},
		{"query auth", "summary?Authorization=secret", ``, nil, 400},
		{"duplicate limit", "rewards?limit=1&limit=2", ``, nil, 400},
		{"unknown query", "rewards?role=admin", ``, nil, 400},
		{"bad limit", "rewards?limit=101", ``, nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			router, _, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) })
			method := http.MethodGet
			if strings.HasPrefix(tc.operation, "actions") {
				method = http.MethodPost
			}
			r := communityTestRequest(method, tc.operation, tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status || calls != 0 || guard.seen != "" {
				t.Fatalf("status=%d want=%d calls=%d guard=%q body=%s", w.Code, tc.status, calls, guard.seen, w.Body.String())
			}
		})
	}
}

func TestCommunityBFFFailsClosedForUnavailableAccountOrBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"unbound", errCommunityBindingRequired, 403},
		{"suspended", errCommunityAccountUnavailable, 403},
		{"guard missing", errors.New("missing trigger with private DSN"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			router, _, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
			guard.err = tc.err
			w := httptest.NewRecorder()
			router.ServeHTTP(w, communityTestRequest("GET", "summary", ""))
			if w.Code != tc.status || calls != 0 || strings.Contains(w.Body.String(), "DSN") {
				t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
			}
		})
	}
}

func TestCommunityBFFProjectsSummaryAndQueriesOriginalAction(t *testing.T) {
	var paths []string
	router, _, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if strings.HasSuffix(r.URL.Path, "summary") {
			_, _ = w.Write([]byte(`{"user_id":42,"level":{"key":"pulse","name":"Pulse"},"available_tickets":3,"lifetime_contribution_milli":1000,"current_contribution_milli":200,"current_period":null,"ledger":[{"source_ref":"private"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"rewards":[{"grant_id":"pg_1","period_id":9,"action_id":"old_action","reward_type":"quota","amount":100,"status":"granted","created_at":"2026-09-19T00:00:00Z","random_value":"private"}]}`))
	})
	for _, op := range []string{"summary", "rewards?limit=10&action_id=old_action"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, communityTestRequest("GET", op, ""))
		if w.Code != 200 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "user_id") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
	if len(paths) != 2 || paths[1] != "/v1/internal/me/rewards?action_id=old_action&limit=10" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestCommunityBFFDoesNotRetryUncertainAction(t *testing.T) {
	calls := 0
	router, _, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"private database failure"}`))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, communityTestRequest("POST", "actions", `{"action_id":"original"}`))
	if w.Code != 503 || calls != 1 || !strings.Contains(w.Body.String(), "action_pending") || strings.Contains(w.Body.String(), "database") {
		t.Fatalf("status=%d calls=%d body=%s", w.Code, calls, w.Body.String())
	}
}

func TestCommunityBFFOnlyClearsDefinitelyRejectedActions(t *testing.T) {
	for _, tc := range []struct {
		upstream, expected string
		status             int
	}{
		{"insufficient_tickets", "action_rejected", 409},
		{"budget_exceeded", "action_rejected", 409},
		{"invalid_action", "action_rejected", 400},
		{"idempotency_conflict", "action_conflict", 409},
		{"database internal detail", "action_conflict", 409},
		{"actions_unavailable", "action_pending", 503},
	} {
		t.Run(tc.upstream, func(t *testing.T) {
			router, _, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": tc.upstream})
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, communityTestRequest("POST", "actions", `{"action_id":"original"}`))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"error":"`+tc.expected+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCommunityProjectionRejectsForeignSummaryAndTrailingJSON(t *testing.T) {
	for _, data := range []string{`{"user_id":999}`, `{"user_id":42}{}`, `null`} {
		if _, err := communityProjection("summary", "42", []byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	result, err := communityProjection("actions", "42", []byte(`{"grant_id":"pg_1","period_id":1,"action_id":"a","random_value":"hidden"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "random_value") {
		t.Fatal("random leaked")
	}
}

func TestCommunityRequiresActualAnswerSessionType(t *testing.T) {
	type UserCacheInfo struct {
		UserID                  string
		UserStatus, EmailStatus int
	}
	for _, value := range []any{nil, map[string]any{"UserID": "7"}, &UserCacheInfo{"7", 1, 1}, "7", new(int)} {
		if _, err := answerSessionValueUserID(value); err == nil {
			t.Fatalf("accepted fake session %T", value)
		}
	}
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = communityTestRequest("GET", "summary", "")
	c.Request.Header.Set(pulseHeaderUserID, "7")
	if _, err := answerSessionUserID(c); err == nil {
		t.Fatal("header created session identity")
	}
}

func TestCommunityOptionalConfigurationPreservesLocalLogin(t *testing.T) {
	cfg := validPluginConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if !(&UserCenter{Config: cfg}).Description().EnabledOriginalUserSystem {
		t.Fatal("local login disabled")
	}
	router, uc, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unconfigured upstream called") })
	uc.Config.CommunityBFFHMACSecret = ""
	w := httptest.NewRecorder()
	router.ServeHTTP(w, communityTestRequest("GET", "summary", ""))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
}
