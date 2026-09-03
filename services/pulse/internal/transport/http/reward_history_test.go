package transporthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type rewardHistoryStub struct {
	userID uint64
	limit  int
}

func (s *rewardHistoryStub) List(_ context.Context, userID uint64, limit int) ([]service.RewardHistoryItem, error) {
	s.userID, s.limit = userID, limit
	return []service.RewardHistoryItem{{GrantID: "grant-1", Amount: 10}}, nil
}

func TestRewardHistoryRouteDerivesPrincipalAndDefaultLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &rewardHistoryStub{}
	router := gin.New()
	RewardHistoryRoute(router.Group("/v1/internal"), stub, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/me/rewards?limit=3&user_id=999", nil))
	if response.Code != http.StatusOK || stub.userID != 7 || stub.limit != 3 {
		t.Fatalf("status=%d user=%d limit=%d body=%s", response.Code, stub.userID, stub.limit, response.Body.String())
	}
}

func TestRewardHistoryRouteRejectsMissingPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RewardHistoryRoute(router.Group("/v1/internal"), &rewardHistoryStub{}, func(c *gin.Context) { c.Next() })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/me/rewards", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
}
