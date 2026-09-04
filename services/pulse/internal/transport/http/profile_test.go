package transporthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/level"
	"github.com/nanashiwang/meta-pulse/internal/security"
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
		c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: "forum"})
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/users/99/profile", nil))
	if response.Code != http.StatusOK || response.Body.String() == "" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		UserID               uint64 `json:"user_id"`
		LifetimeContribution int64  `json:"lifetime_contribution_milli"`
		Level                struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"level"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.UserID != 7 || body.LifetimeContribution != 2000 || body.Level.Key != "pulse" || body.Level.Name != "脉冲者" {
		t.Fatalf("unexpected profile contract: %+v", body)
	}
}

func TestProfileRouteRejectsNonForumRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ProfileRoute(router.Group("/v1/internal"), profileStub{}, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: "new-api"})
		c.Next()
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/users/42/profile", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
}
