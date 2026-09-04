package transporthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type summaryStub struct {
	seenUserID uint64
}

func (s *summaryStub) GetSummary(_ context.Context, userID uint64, _ time.Time) (service.PulseSummary, error) {
	s.seenUserID = userID
	return service.PulseSummary{Profile: service.UserProfile{UserID: userID, Level: level.Result{Key: "new", Name: "新用户"}}}, nil
}

func TestSummaryRouteUsesAuthenticatedPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &summaryStub{}
	router := gin.New()
	SummaryRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/me/summary", nil))
	if response.Code != http.StatusOK || reader.seenUserID != 7 {
		t.Fatalf("status=%d user=%d body=%s", response.Code, reader.seenUserID, response.Body.String())
	}
	if response.Body.String() == `{"user_id":99}` {
		t.Fatal("summary used a client-supplied identity")
	}
}

func TestSummaryRouteRejectsMissingPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SummaryRoute(router.Group("/v1/internal"), &summaryStub{}, func(c *gin.Context) { c.Next() })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/me/summary", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestSummaryRouteRejectsNonNewAPIRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SummaryRoute(router.Group("/v1/internal"), &summaryStub{}, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "forum"})
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/me/summary", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
}
