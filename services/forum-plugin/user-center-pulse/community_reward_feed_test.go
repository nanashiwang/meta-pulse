package pulse_user_center

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommunityRewardFeedIdentityAndAllowlist(t *testing.T) {
	router, _, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/internal/community/rewards" || r.Header.Get(pulseHeaderRole) != "community-bff" || r.Header.Get(pulseHeaderUserID) != "42" {
			t.Error("wrong endpoint or principal")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("browser credentials leaked")
		}
		_, _ = w.Write([]byte(`{"quota_per_unit":500000,"user_id":999,"rewards":[{"reward_type":"newapi_quota","amount":625000,"won_at":"2026-10-15T03:15:49Z","user_id":999,"grant_id":"private","action_id":"private","username":"private","avatar":"https://private","source_ref":"private"}]}`))
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, communityTestRequest("GET", "winners", ""))
	if w.Code != 200 || guard.seen != "7" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "user_id") || !strings.Contains(w.Body.String(), "03:15:00Z") {
		t.Fatal("projection leak", w.Body.String())
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(w.Body.Bytes(), &value) != nil || len(value) != 2 {
		t.Fatal("unexpected fields")
	}
}
func TestCommunityRewardFeedRejectsQueriesAndInvalidIdentity(t *testing.T) {
	for _, operation := range []string{"winners?user_id=1", "winners?limit=20", "winners?cursor=2", "winners?Authorization=secret"} {
		calls := 0
		router, _, guard := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { calls++ })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, communityTestRequest("GET", operation, ""))
		if w.Code != 400 || calls != 0 || guard.seen != "" {
			t.Fatal(operation, w.Code, calls)
		}
	}
	for _, failure := range []error{errCommunityAccountUnavailable, errCommunityBindingRequired, errCommunityUnauthenticated} {
		calls := 0
		router, _, guard := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { calls++ })
		guard.err = failure
		w := httptest.NewRecorder()
		router.ServeHTTP(w, communityTestRequest("GET", "winners", ""))
		if (w.Code != 401 && w.Code != 403) || calls != 0 {
			t.Fatal(w.Code, calls)
		}
	}
	calls := 0
	router, _, _ := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { calls++ })
	req := communityTestRequest("GET", "winners", "")
	req.Header.Del("Authorization")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 401 || calls != 0 {
		t.Fatal("missing session allowed")
	}
}
func TestCommunityRewardFeedRejectsMalformedUpstream(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `{"quota_per_unit":null,"rewards":[]}`, `{"quota_per_unit":1,"rewards":null}`, `{"quota_per_unit":-1,"rewards":[]}`, `{"quota_per_unit":1,"rewards":[{"amount":1,"reward_type":"cash","won_at":"2026-10-15T00:00:00Z"}]}`, `{"quota_per_unit":1,"rewards":[{"amount":0,"reward_type":"community_exp","won_at":"2026-10-15T00:00:00Z"}]}`} {
		router, _, _ := communityTestRouter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
		w := httptest.NewRecorder()
		router.ServeHTTP(w, communityTestRequest("GET", "winners", ""))
		if w.Code != 503 {
			t.Fatal("malformed response accepted", body, w.Code)
		}
	}
}

type namedFeedGuard struct {
	adminTestGuard
	calls int
}

func (g *namedFeedGuard) CommunityUsernames(_ context.Context, ids []uint64) (map[uint64]string, error) {
	g.calls++
	if len(ids) != 1 || ids[0] != 42 {
		return nil, fmt.Errorf("wrong external identity")
	}
	return map[uint64]string{42: "winner-name"}, nil
}
func TestAdminRewardFeedIdentityBoundary(t *testing.T) {
	for _, denied := range []bool{false, true} {
		calls := 0
		router, uc, _ := adminTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/v1/internal/admin/community/rewards" || r.Header.Get(pulseHeaderRole) != "admin" || r.Header.Get(pulseHeaderUserID) != "7" {
				t.Error("wrong admin principal")
			}
			_, _ = w.Write([]byte(`{"quota_per_unit":500000,"rewards":[{"user_id":42,"username":"upstream-name","grant_id":"private","reward_type":"community_exp","amount":10,"won_at":"2026-11-09T00:00:59Z"}]}`))
		}, nil)
		guard := &namedFeedGuard{}
		if denied {
			guard.err = errAdminForbidden
		}
		uc.Guard = guard
		w := httptest.NewRecorder()
		router.ServeHTTP(w, adminTestRequest("GET", "winners", ""))
		if denied {
			if w.Code != 403 || calls != 0 || guard.calls != 0 {
				t.Fatal("demoted administrator read names", w.Code)
			}
			continue
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), "winner-name") || strings.Contains(w.Body.String(), "user_id") || strings.Contains(w.Body.String(), "upstream-name") || strings.Contains(w.Body.String(), "private") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestAdminRewardFeedRejectsSessionAndQuery(t *testing.T) {
	for _, sessionErr := range []error{errAdminForbidden, errCommunityUnauthenticated} {
		router, _, _ := adminTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Fatal("unauthorized upstream call") }, sessionErr)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, adminTestRequest("GET", "winners", ""))
		if w.Code != 401 && w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	router, _, _ := adminTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Fatal("query upstream call") }, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, adminTestRequest("GET", "winners?user_id=42", ""))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}
