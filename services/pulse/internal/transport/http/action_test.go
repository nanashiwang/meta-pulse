package transporthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type actionStub struct{ command service.ActionCommand }

func (s *actionStub) Execute(_ context.Context, command service.ActionCommand) (service.ActionResult, error) {
	s.command = command
	return service.ActionResult{GrantID: "pg_1", UserID: command.UserID, ActionID: command.ActionID, Status: service.RewardStatusPending}, nil
}

func TestActionRouteDerivesIdentityAndRequiresIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &actionStub{}
	router := gin.New()
	ActionRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
		c.Next()
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a1","trigger_type":"pulse"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || reader.command.UserID != 7 || reader.command.ActionID != "a1" || reader.command.IdempotencyKey != "request-1" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, reader.command, response.Body.String())
	}
}

func TestActionRouteRejectsMissingIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ActionRoute(router.Group("/v1/internal"), &actionStub{}, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
		c.Next()
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a1","trigger_type":"pulse"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestActionRouteRejectsNonNewAPIRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ActionRoute(router.Group("/v1/internal"), &actionStub{}, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "forum"})
		c.Next()
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a1","trigger_type":"pulse"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestActionRouteRejectsTrailingJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reader := &actionStub{}
	router := gin.New()
	ActionRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
		c.Next()
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a1","trigger_type":"pulse"} {"unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "request-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if reader.command.ActionID != "" {
		t.Fatalf("executor received rejected request: %+v", reader.command)
	}
}

func TestActionRouteRejectsForgedTriggerTypeBeforeExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, triggerType := range []string{"", "content", "period_reward", "forged"} {
		t.Run(triggerType, func(t *testing.T) {
			executor := &actionStub{}
			router := gin.New()
			ActionRoute(router.Group("/v1/internal"), executor, func(c *gin.Context) {
				c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "new-api"})
				c.Next()
			})
			body := `{"action_id":"a1","trigger_type":"` + triggerType + `"}`
			request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "request-1")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || executor.command.ActionID != "" {
				t.Fatalf("trigger=%q status=%d command=%+v body=%s", triggerType, response.Code, executor.command, response.Body.String())
			}
		})
	}
}
