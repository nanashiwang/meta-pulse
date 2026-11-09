package pulse_user_center

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type ticketTestGuard struct {
	adminTestGuard
	target    string
	targetErr error
	lookups   []string
}

func (g *ticketTestGuard) adminTicketTarget(_ context.Context, username string) (string, error) {
	g.lookups = append(g.lookups, username)
	return g.target, g.targetErr
}

func ticketTestRouter(t *testing.T, upstream http.HandlerFunc) (*gin.Engine, *ticketTestGuard, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	old := ticketGrantEnabled
	ticketGrantEnabled = func() bool { return true }
	t.Cleanup(func() { ticketGrantEnabled = old })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; upstream(w, r) }))
	t.Cleanup(server.Close)
	cfg := validPluginConfig()
	cfg.PulseBaseURL = server.URL
	cfg.AdminHMACSecret = strings.Repeat("a", 32)
	guard := &ticketTestGuard{target: "4242"}
	uc := &UserCenter{Config: cfg, Client: NewPulseClient(cfg), Guard: guard}
	router := gin.New()
	uc.registerAdminTicketGrant(router.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	return router, guard, &calls
}

func ticketTestRequest(body string) *http.Request {
	r := communityTestRequest(http.MethodPost, "x", body)
	r.URL.Path = "/answer/admin/api/metar/accounts/tickets"
	return r
}

func TestAdminTicketGrantResolvesBindingAndSignsAsAdmin(t *testing.T) {
	router, guard, calls := ticketTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(data, &got)
		if r.URL.Path != "/v1/internal/admin/tickets/grant" || r.Header.Get(pulseHeaderRole) != "admin" || r.Header.Get(pulseHeaderUserID) != "7" || r.Header.Get("Idempotency-Key") != "a7-request_1" {
			t.Errorf("bad upstream request %s %v", r.URL.Path, r.Header)
		}
		if got["user_id"] != float64(4242) || got["count"] != float64(10) || got["reason"] != "验证新概率" || got["username"] != nil {
			t.Errorf("payload %s", data)
		}
		_, _ = w.Write([]byte(`{"entry_id":5,"count":10,"quota_expires_at":"2026-12-09T00:00:00Z"}`))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, ticketTestRequest(`{"username":"uno","count":10,"reason":" 验证新概率 "}`))
	if w.Code != 200 || *calls != 1 || guard.seen != "7" || len(guard.lookups) != 1 || guard.lookups[0] != "uno" || !strings.Contains(w.Body.String(), `"count":10`) {
		t.Fatalf("%d %s calls=%d guard=%+v", w.Code, w.Body.String(), *calls, guard)
	}
}

func TestAdminTicketGrantRejectsBeforeUpstream(t *testing.T) {
	for name, c := range map[string]struct {
		body   string
		mutate func(*http.Request, *ticketTestGuard)
		status int
	}{
		"too many":      {`{"username":"uno","count":101,"reason":"验证"}`, nil, 400},
		"zero":          {`{"username":"uno","count":0,"reason":"验证"}`, nil, 400},
		"no reason":     {`{"username":"uno","count":1,"reason":" "}`, nil, 400},
		"user id field": {`{"username":"uno","count":1,"reason":"验证","user_id":1}`, nil, 400},
		"no key":        {`{"username":"uno","count":1,"reason":"验证"}`, func(r *http.Request, _ *ticketTestGuard) { r.Header.Del("Idempotency-Key") }, 400},
		"cross site":    {`{"username":"uno","count":1,"reason":"验证"}`, func(r *http.Request, _ *ticketTestGuard) { r.Header.Set("Origin", "https://evil.test") }, 403},
		"not admin":     {`{"username":"uno","count":1,"reason":"验证"}`, func(_ *http.Request, g *ticketTestGuard) { g.err = errAdminForbidden }, 403},
		"unbound":       {`{"username":"uno","count":1,"reason":"验证"}`, func(_ *http.Request, g *ticketTestGuard) { g.targetErr = errCommunityBindingRequired }, 409},
		"missing user":  {`{"username":"ghost","count":1,"reason":"验证"}`, func(_ *http.Request, g *ticketTestGuard) { g.targetErr = errCommunityAccountUnavailable }, 404},
	} {
		t.Run(name, func(t *testing.T) {
			router, guard, calls := ticketTestRouter(t, func(w http.ResponseWriter, _ *http.Request) { t.Error("upstream called") })
			r := ticketTestRequest(c.body)
			if c.mutate != nil {
				c.mutate(r, guard)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != c.status || *calls != 0 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
