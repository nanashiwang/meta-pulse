package transporthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type actionStub struct {
	command service.ActionCommand
	bonus   int64
}

func (s *actionStub) Execute(_ context.Context, command service.ActionCommand) (service.ActionResult, error) {
	s.command = command
	result := service.ActionResult{GrantID: "pg_1", PeriodID: 1, UserID: command.UserID, ActionID: command.ActionID, RewardType: "newapi_quota", Amount: 500000, Status: service.RewardStatusPending}
	if s.bonus > 0 {
		result.BonusExperience = s.bonus
		result.BonusExperienceStatus = "pending"
	}
	return result, nil
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

func TestActionRoutePreservesV2Selection(t *testing.T) {
	reader := &actionStub{}
	router := gin.New()
	ActionRoute(router.Group("/v1/internal"), reader, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "community-bff"})
		c.Next()
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a2","trigger_type":"pulse","protocol_version":2,"selection":"original-server-choice"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "v2-key")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 201 || reader.command.ProtocolVersion != 2 || reader.command.Selection != "original-server-choice" || reader.command.UserID != 7 {
		t.Fatalf("selection was changed: %+v / %d", reader.command, response.Code)
	}
}

func TestActionResponseIncludesConfirmedBonusWithoutPrivateFields(t *testing.T) {
	for _, bonus := range []int64{4, 0} {
		router := gin.New()
		ActionRoute(router.Group("/v1/internal"), &actionStub{bonus: bonus}, func(c *gin.Context) {
			c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: "community-bff"})
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/internal/me/actions", strings.NewReader(`{"action_id":"a1","trigger_type":"pulse"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "request-1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		var body map[string]json.RawMessage
		if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &body) != nil {
			t.Fatalf("unexpected action result: %d %s", response.Code, response.Body.String())
		}
		if bonus > 0 && (string(body["bonus_experience"]) != "4" || string(body["bonus_experience_status"]) != `"pending"`) {
			t.Fatalf("confirmed bonus missing: %s", response.Body.String())
		}
		if bonus == 0 && (body["bonus_experience"] != nil || body["bonus_experience_status"] != nil) {
			t.Fatalf("no bonus should be shown: %s", response.Body.String())
		}
		if body["user_id"] != nil || body["random_value"] != nil {
			t.Fatal("private fields leaked")
		}
	}
}
