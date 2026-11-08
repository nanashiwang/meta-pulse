package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const intelTestKey = "0f8fad5b-d9cb-469f-a165-70867728950e"

func intelTestRouter(t *testing.T, upstream http.HandlerFunc, sessionErr error) (*gin.Engine, *adminTestGuard) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	secret := strings.Repeat("i", 32)
	previous := intelAdminConfig
	intelAdminConfig = func() intelAdminTarget { return intelAdminTarget{BaseURL: server.URL, Secret: secret} }
	t.Cleanup(func() { intelAdminConfig = previous })
	guard := &adminTestGuard{}
	uc := &UserCenter{Config: validPluginConfig(), Guard: guard}
	router := gin.New()
	uc.registerIntelAdmin(router.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", sessionErr }, func() string { return "https://metar.example.test" })
	return router, guard
}

func intelTestRequest(method, op, body string) *http.Request {
	r := httptest.NewRequest(method, "/answer/admin/api/metar/intel/"+op, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer answer-session-secret")
	r.Header.Set("Origin", "https://metar.example.test")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Metar-Request", "1")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if op == "import" {
		r.Header.Set("Idempotency-Key", intelTestKey)
	}
	return r
}

func TestIntelAdminSignsFixedPathWithServerIdentity(t *testing.T) {
	doc := `{"source":{"slug":"manual"}}`
	var calls int
	router, guard := intelTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/v1/internal/admin/import" || string(body) != doc || r.URL.RawQuery != "" {
			t.Errorf("upstream path/body: %s %s", r.URL.Path, body)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials forwarded")
		}
		if r.Header.Get("X-Intel-User-Id") != "7" || r.Header.Get("X-Intel-Role") != "admin" || r.Header.Get("Idempotency-Key") != intelTestKey {
			t.Error("identity or key missing")
		}
		ts, _ := strconv.ParseInt(r.Header.Get("X-Intel-Timestamp"), 10, 64)
		mac := hmac.New(sha256.New, []byte(strings.Repeat("i", 32)))
		mac.Write([]byte(pulseCanonicalPayload("POST", r.URL.EscapedPath(), "7", "admin", ts, r.Header.Get("X-Intel-Nonce"), body)))
		if r.Header.Get("X-Intel-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("signature does not verify")
		}
		w.Write([]byte(`{"stats":{"run_id":1}}`))
	}, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, intelTestRequest("POST", "import", doc))
	if w.Code != 200 || calls != 1 || guard.seen != "7" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d calls %d guard %q", w.Code, calls, guard.seen)
	}
}

func TestIntelAdminRejectsBeforeUpstream(t *testing.T) {
	calls := 0
	router, guard := intelTestRouter(t, func(w http.ResponseWriter, r *http.Request) { calls++ }, nil)
	cases := map[string]func() *http.Request{
		"query": func() *http.Request {
			r := intelTestRequest("GET", "overview", "")
			r.URL.RawQuery = "x=1"
			return r
		},
		"no marker": func() *http.Request {
			r := intelTestRequest("GET", "overview", "")
			r.Header.Del("X-Metar-Request")
			return r
		},
		"no auth": func() *http.Request {
			r := intelTestRequest("GET", "overview", "")
			r.Header.Del("Authorization")
			return r
		},
		"cross origin": func() *http.Request {
			r := intelTestRequest("POST", "validate", `{}`)
			r.Header.Set("Origin", "https://evil.example")
			return r
		},
		"not json": func() *http.Request {
			r := intelTestRequest("POST", "validate", `[1]`)
			return r
		},
		"no key": func() *http.Request {
			r := intelTestRequest("POST", "import", `{}`)
			r.Header.Del("Idempotency-Key")
			return r
		},
		"recompute body": func() *http.Request { return intelTestRequest("POST", "recompute", `{"x":1}`) },
		"too large": func() *http.Request {
			return intelTestRequest("POST", "validate", `{"a":"`+strings.Repeat("x", maxIntelAdminRequest)+`"}`)
		},
	}
	for name, mk := range cases {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, mk())
		if w.Code < 400 {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("upstream called %d times", calls)
	}
	guard.err = errAdminForbidden
	w := httptest.NewRecorder()
	router.ServeHTTP(w, intelTestRequest("GET", "overview", ""))
	if w.Code != 403 || calls != 0 {
		t.Fatalf("demoted admin: %d calls %d", w.Code, calls)
	}
}

func TestIntelAdminSessionErrorsAndUpstreamMapping(t *testing.T) {
	router, _ := intelTestRouter(t, func(w http.ResponseWriter, r *http.Request) {}, errCommunityUnauthenticated)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, intelTestRequest("GET", "overview", ""))
	if w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}

	for _, c := range []struct {
		status int
		body   string
		want   int
		has    string
	}{
		{422, `{"error":"invalid_document","problems":["models[0].name bad"]}`, 422, "models[0].name bad"},
		{409, `{"error":"idempotency_conflict"}`, 409, "idempotency_conflict"},
		{422, `{"error":"secret detail","problems":["x"]}`, 503, "intel_unavailable"},
		{500, `{"error":"db exploded at host x"}`, 503, "intel_unavailable"},
		{401, `{"error":"unauthorized"}`, 503, "intel_unavailable"},
	} {
		router, _ := intelTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, intelTestRequest("POST", "validate", `{}`))
		if w.Code != c.want || !strings.Contains(w.Body.String(), c.has) || strings.Contains(w.Body.String(), "host x") {
			t.Errorf("upstream %d: got %d %s", c.status, w.Code, w.Body.String())
		}
	}
}

func TestIntelAdminUnconfigured(t *testing.T) {
	router, _ := intelTestRouter(t, func(w http.ResponseWriter, r *http.Request) { t.Error("must not call upstream") }, nil)
	intelAdminConfig = func() intelAdminTarget { return intelAdminTarget{BaseURL: "http://intel-api:8090", Secret: "short"} }
	w := httptest.NewRecorder()
	router.ServeHTTP(w, intelTestRequest("GET", "overview", ""))
	if w.Code != 503 {
		t.Fatalf("short secret: %d", w.Code)
	}
	intelAdminConfig = func() intelAdminTarget {
		return intelAdminTarget{BaseURL: "http://user:pw@intel-api:8090", Secret: strings.Repeat("s", 32)}
	}
	w = httptest.NewRecorder()
	router.ServeHTTP(w, intelTestRequest("GET", "overview", ""))
	if w.Code != 503 {
		t.Fatalf("credentialed URL: %d", w.Code)
	}
	if !errors.Is(errIntelAdminUnavailable, errIntelAdminUnavailable) {
		t.Fatal("sentinel")
	}
}
