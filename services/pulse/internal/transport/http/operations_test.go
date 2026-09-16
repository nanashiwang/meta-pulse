package transporthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type overviewStub struct {
	overview service.OperationsOverview
	err      error
	calls    int
}

func (s *overviewStub) Load(context.Context) (service.OperationsOverview, error) {
	s.calls++
	return s.overview, s.err
}

func overviewRouter(stub *overviewStub, role string, userID uint64) *gin.Engine {
	router := gin.New()
	OperationsOverviewRoute(router.Group("/v1/internal"), stub, func(c *gin.Context) {
		c.Set(PrincipalContextKey, security.Principal{UserID: userID, Role: role})
		c.Next()
	})
	return router
}

func getOverview(router *gin.Engine) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/internal/admin/operations/overview", nil))
	return response
}

func TestOperationsOverviewRouteReturnsProjectionForAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &overviewStub{overview: service.OperationsOverview{
		ObservedAt: time.Unix(1789000000, 0).UTC(),
		Periods:    []service.PeriodOverviewItem{{ID: 1, Key: "2026-P001", Status: "active", RuleCount: 1}},
		Health:     service.OperationsOverviewHealth{HasActivePeriod: true},
	}}
	response := getOverview(overviewRouter(stub, "admin", 7))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var decoded service.OperationsOverview
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Periods) != 1 || decoded.Periods[0].Key != "2026-P001" {
		t.Fatalf("decoded periods = %+v", decoded.Periods)
	}
	if !decoded.Health.HasActivePeriod {
		t.Fatal("health was not carried through the route")
	}
}

// The projection names periods, cursors and ticket counts. A non-admin
// principal must not be able to read it even though the route never writes.
func TestOperationsOverviewRouteRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{"new-api", "user", ""} {
		stub := &overviewStub{}
		response := getOverview(overviewRouter(stub, role, 7))
		if response.Code != http.StatusForbidden {
			t.Fatalf("role %q status = %d, want %d", role, response.Code, http.StatusForbidden)
		}
		if stub.calls != 0 {
			t.Fatalf("role %q reached the service", role)
		}
	}
}

// A signed request carrying the admin role but no user id is not an identity.
func TestOperationsOverviewRouteRejectsAnonymousAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &overviewStub{}
	if response := getOverview(overviewRouter(stub, "admin", 0)); response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if stub.calls != 0 {
		t.Fatal("an anonymous principal reached the service")
	}
}

// The error text can name internal tables and columns; the console only needs
// to know the projection is unavailable.
func TestOperationsOverviewRouteHidesInternalErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &overviewStub{err: errors.New("list period overview: Table 'meta_pulse.pulse_period' doesn't exist")}
	response := getOverview(overviewRouter(stub, "admin", 7))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if body := response.Body.String(); strings.Contains(body, "pulse_period") || strings.Contains(body, "Table") {
		t.Fatalf("internal detail leaked: %s", body)
	}
}

// The console polls this endpoint; a cached page would show a stalled cursor
// as healthy.
func TestOperationsOverviewRouteIsNotCacheable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := getOverview(overviewRouter(&overviewStub{}, "admin", 7))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

// The route is read-only by construction: no mutating verb is registered.
func TestOperationsOverviewRouteRejectsMutatingVerbs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := overviewRouter(&overviewStub{}, "admin", 7)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, "/v1/internal/admin/operations/overview", nil))
		if response.Code == http.StatusOK {
			t.Fatalf("%s was accepted on a read-only route", method)
		}
	}
}
