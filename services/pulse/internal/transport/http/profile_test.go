package transporthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type profileStub struct{}

func (profileStub) Get(context.Context, uint64) (service.UserProfile, error) {
	return service.UserProfile{UserID: 7, LifetimeContribution: 2000, Level: level.Result{Key: "pulse", Name: "脉冲者"}}, nil
}

func TestProfileRouteRequiresAuthAndDoesNotTrustClientIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ProfileRoute(router.Group("/v1/internal"), profileStub{}, func(c *gin.Context) {
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/users/99/profile", nil))
	if response.Code != http.StatusOK || response.Body.String() == "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.String() == `{"user_id":99}` {
		t.Fatal("handler trusted client path identity")
	}
}
