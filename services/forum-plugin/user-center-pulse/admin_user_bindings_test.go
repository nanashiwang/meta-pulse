package pulse_user_center

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type bindingTestGuard struct {
	adminTestGuard
	asked     [][]string
	lookupErr error
}

func (g *bindingTestGuard) adminUserBindings(_ context.Context, ids []string) (map[string]adminBindingInfo, error) {
	g.asked = append(g.asked, ids)
	return map[string]adminBindingInfo{"12": {APIUserID: "4242", Providers: []string{"google"}}}, g.lookupErr
}

func bindingTestRouter(t *testing.T, guard *bindingTestGuard) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	old := ticketGrantEnabled
	ticketGrantEnabled = func() bool { return true }
	t.Cleanup(func() { ticketGrantEnabled = old })
	uc := &UserCenter{Config: validPluginConfig(), Guard: guard}
	router := gin.New()
	uc.registerAdminUserBindings(router.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	return router
}

func bindingTestRequest(body string) *http.Request {
	r := communityTestRequest(http.MethodPost, "x", body)
	r.URL.Path = "/answer/admin/api/metar/accounts/bindings"
	return r
}

func TestAdminUserBindingsChecksAdminAndDeduplicates(t *testing.T) {
	guard := &bindingTestGuard{}
	w := httptest.NewRecorder()
	bindingTestRouter(t, guard).ServeHTTP(w, bindingTestRequest(`{"user_ids":["12","13","12"]}`))
	if w.Code != 200 || guard.seen != "7" || len(guard.asked) != 1 || strings.Join(guard.asked[0], ",") != "12,13" {
		t.Fatalf("%d %s guard=%+v", w.Code, w.Body.String(), guard)
	}
	if w.Body.String() != `{"data":{"12":{"api_user_id":"4242","providers":["google"]}}}` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("body %s", w.Body.String())
	}
}

func TestAdminUserBindingsRejectsBeforeLookup(t *testing.T) {
	many := `{"user_ids":["1"` + strings.Repeat(`,"1"`, maxAdminBindingLookup) + `]}`
	for name, c := range map[string]struct {
		body   string
		mutate func(*http.Request, *bindingTestGuard)
		status int
	}{
		"trailing object":      {`{"user_ids":["12"]} {"extra":true}`, nil, 400},
		"trailing bracket":     {`{"user_ids":["12"]}]`, nil, 400},
		"trailing brace":       {`{"user_ids":["12"]}}`, nil, 400},
		"oversized whitespace": {`{"user_ids":["12"]}` + strings.Repeat(" ", 4096), nil, 400},
		"empty":                {`{"user_ids":[]}`, nil, 400},
		"too many":             {many, nil, 400},
		"not canonical":        {`{"user_ids":["012"]}`, nil, 400},
		"not numeric":          {`{"user_ids":["1 OR 1"]}`, nil, 400},
		"unknown field":        {`{"user_ids":["1"],"all":true}`, nil, 400},
		"cross origin":         {`{"user_ids":["1"]}`, func(r *http.Request, _ *bindingTestGuard) { r.Header.Set("Origin", "https://evil.example") }, 403},
		"no marker":            {`{"user_ids":["1"]}`, func(r *http.Request, _ *bindingTestGuard) { r.Header.Del("X-Metar-Request") }, 403},
		"query":                {`{"user_ids":["1"]}`, func(r *http.Request, _ *bindingTestGuard) { r.URL.RawQuery = "x=1" }, 403},
		"stale admin":          {`{"user_ids":["1"]}`, func(_ *http.Request, g *bindingTestGuard) { g.err = errAdminForbidden }, 403},
	} {
		t.Run(name, func(t *testing.T) {
			guard := &bindingTestGuard{}
			r := bindingTestRequest(c.body)
			if c.mutate != nil {
				c.mutate(r, guard)
			}
			w := httptest.NewRecorder()
			bindingTestRouter(t, guard).ServeHTTP(w, r)
			if w.Code != c.status || len(guard.asked) != 0 {
				t.Fatalf("%d %s asked=%v", w.Code, w.Body.String(), guard.asked)
			}
		})
	}
}

func TestAdminUserBindingsLookupFailureIsUnavailable(t *testing.T) {
	guard := &bindingTestGuard{lookupErr: errors.New("db down")}
	w := httptest.NewRecorder()
	bindingTestRouter(t, guard).ServeHTTP(w, bindingTestRequest(`{"user_ids":["12"]}`))
	if w.Code != 503 || strings.Contains(w.Body.String(), "4242") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

type statisticsBindingGuard struct{ bindingTestGuard }

func (*statisticsBindingGuard) adminCommunityStatistics(_ context.Context, ids []string) (map[string]*adminCommunityStatistics, error) {
	result := map[string]*adminCommunityStatistics{}
	for _, id := range ids {
		result[id] = &adminCommunityStatistics{Posts: 2, PostLikes: 3, Comments: 4, Likes: 5, Shares: 6}
	}
	return result, nil
}
func TestAdminUserStatisticsPulseFailureKeepsCommunityCounts(t *testing.T) {
	old := ticketGrantEnabled
	ticketGrantEnabled = func() bool { return true }
	defer func() { ticketGrantEnabled = old }()
	for _, status := range []int{200, 503} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/internal/admin/tickets/statistics" || r.Header.Get(pulseHeaderSignature) == "" {
				t.Error("missing fixed signed request")
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"4242":{"remaining":"8","total":"20"}}`))
		}))
		cfg := validPluginConfig()
		cfg.PulseBaseURL = upstream.URL
		cfg.AdminHMACSecret = strings.Repeat("a", 32)
		uc := &UserCenter{Config: cfg, Client: NewPulseClient(cfg), Guard: &statisticsBindingGuard{}}
		r := gin.New()
		uc.registerAdminUserBindings(r.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, bindingTestRequest(`{"user_ids":["12","13"]}`))
		upstream.Close()
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"posts":"2"`) || !strings.Contains(w.Body.String(), `"shares":"6"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `"tickets"`) != (status == 200) {
			t.Fatalf("%s", w.Body.String())
		}
	}
}
