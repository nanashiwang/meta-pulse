package transporthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type feedReader struct {
	calls int
	err   error
}

func (r *feedReader) Recent(context.Context) (service.RewardFeed, error) {
	r.calls++
	return service.RewardFeed{Rewards: []service.RewardFeedItem{}, QuotaPerUnit: 500000}, r.err
}
func TestRewardFeedRequiresCommunityRoleAndFixedQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		role, path, method string
		user               uint64
		status             int
	}{
		{"community-bff", "", "GET", 7, 200}, {"community-bff", "?limit=20", "GET", 7, 400}, {"community-bff", "?user_id=8", "GET", 7, 400},
		{"new-api", "", "GET", 7, 401}, {"forum", "", "GET", 7, 401}, {"admin", "", "GET", 7, 401}, {"community-bff", "", "GET", 0, 401}, {"", "", "GET", 7, 401},
		{"community-bff", "", "POST", 7, 404},
	} {
		reader := &feedReader{}
		router := gin.New()
		RewardFeedRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
			c.Set(PrincipalContextKey, security.Principal{UserID: tc.user, Role: tc.role})
			c.Next()
		})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc.method, "/v1/internal/community/rewards"+tc.path, nil))
		if w.Code != tc.status || (tc.status != 200 && reader.calls != 0) {
			t.Fatalf("%+v status=%d calls=%d", tc, w.Code, reader.calls)
		}
		if tc.status == 200 && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"rewards":[]`)) {
			t.Fatal("unsafe response", w.Body.String())
		}
	}
}
func TestRewardFeedFailureDoesNotExposeStoreErrors(t *testing.T) {
	reader := &feedReader{err: errors.New("private database address")}
	router := gin.New()
	RewardFeedRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "community-bff"})
		c.Next()
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/internal/community/rewards", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
}
