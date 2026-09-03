package transporthttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type contentAwardStub struct {
	command    service.ContentAwardCommand
	reversed   []string
	awardErr   error
	reverseErr error
}

func (s *contentAwardStub) ReviewAndAward(_ context.Context, command service.ContentAwardCommand) (service.ContentAwardResult, error) {
	s.command = command
	if s.awardErr != nil {
		return service.ContentAwardResult{}, s.awardErr
	}
	return service.ContentAwardResult{Award: structContentAward(command)}, nil
}

func (s *contentAwardStub) Reverse(_ context.Context, actionID, actorType, actorID, reason, requestID string) error {
	s.reversed = []string{actionID, actorType, actorID, reason, requestID}
	return s.reverseErr
}

// Keep the route test independent from service internals while returning a
// valid JSON response shape.
func structContentAward(command service.ContentAwardCommand) ports.ContentAward {
	return ports.ContentAward{CandidateID: command.CandidateID, AwardVersion: command.AwardVersion, ActionID: "content_award:question:42:1", UserID: 9, Amount: command.Amount, RewardType: command.RewardType, Status: ports.ContentAwardPending}
}

func adminContentRouter(stub *contentAwardStub, role string) *gin.Engine {
	router := gin.New()
	ContentAwardRoute(router.Group("/v1/internal"), stub, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: 7, Role: role})
		c.Next()
	})
	return router
}

func TestContentAwardRouteRequiresAdminAndDerivesActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &contentAwardStub{}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards", strings.NewReader(`{"candidate_id":1,"award_version":1,"reward_type":"quota","amount":10,"reason":"精华"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "review-1")
	response := httptest.NewRecorder()
	adminContentRouter(stub, "admin").ServeHTTP(response, request)
	if response.Code != http.StatusCreated || stub.command.ActorID != "7" || stub.command.ActorType != "admin" || stub.command.RequestID != "review-1" {
		t.Fatalf("status=%d command=%+v body=%s", response.Code, stub.command, response.Body.String())
	}
}

func TestContentAwardRouteRejectsNonAdminAndMissingIdempotency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		role   string
		key    string
		status int
	}{
		{name: "non-admin", role: "new-api", key: "review-1", status: http.StatusForbidden},
		{name: "missing key", role: "admin", status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &contentAwardStub{}
			request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards", strings.NewReader(`{"candidate_id":1,"award_version":1,"reward_type":"quota","amount":10,"reason":"精华"}`))
			request.Header.Set("Content-Type", "application/json")
			if tc.key != "" {
				request.Header.Set("Idempotency-Key", tc.key)
			}
			response := httptest.NewRecorder()
			adminContentRouter(stub, tc.role).ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if stub.command.ActorID != "" {
				t.Fatal("non-accepted request reached executor")
			}
		})
	}
}

func TestContentAwardRouteDoesNotTrustActorInJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &contentAwardStub{}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards", strings.NewReader(`{"candidate_id":1,"award_version":1,"reward_type":"quota","amount":10,"reason":"精华","actor_id":"attacker","user_id":999}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "review-1")
	response := httptest.NewRecorder()
	adminContentRouter(stub, "admin").ServeHTTP(response, request)
	if response.Code != http.StatusCreated || stub.command.ActorID != "7" {
		t.Fatalf("status=%d actor=%q body=%s", response.Code, stub.command.ActorID, response.Body.String())
	}
}

func TestContentAwardReversalRouteUsesStableActionAndRequestKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &contentAwardStub{}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards/content_award:question:42:1/reverse", strings.NewReader(`{"reason":"抄袭"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "reverse-1")
	response := httptest.NewRecorder()
	adminContentRouter(stub, "admin").ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || len(stub.reversed) != 5 || stub.reversed[0] != "content_award:question:42:1" || stub.reversed[2] != "7" || stub.reversed[4] != "reverse-1" {
		t.Fatalf("status=%d reversed=%v body=%s", response.Code, stub.reversed, response.Body.String())
	}
}

func TestContentAwardRouteMapsDomainErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &contentAwardStub{awardErr: service.ErrContentAwardLimit}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards", strings.NewReader(`{"candidate_id":1,"award_version":1,"reward_type":"quota","amount":10,"reason":"精华"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "review-1")
	response := httptest.NewRecorder()
	adminContentRouter(stub, "admin").ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestContentAwardRouteMapsInvalidReversal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &contentAwardStub{reverseErr: errors.New("rollback failed")}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/admin/content-awards/a1/reverse", strings.NewReader(`{"reason":"撤销"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "reverse-1")
	response := httptest.NewRecorder()
	adminContentRouter(stub, "admin").ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
