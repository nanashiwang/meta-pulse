package pulse_user_center

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminSettlementHealthPermissionsAndProjection(t *testing.T) {
	router, _, guard := adminTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/internal/admin/operations/overview" || r.Header.Get(pulseHeaderRole) != "admin" || r.Header.Get(pulseHeaderUserID) != "7" {
			t.Error("wrong signed target")
		}
		fmt.Fprint(w, `{"observed_at":"2026-11-17T00:00:00Z","snapshot":{"settlement_dead_count":2,"settlement_retry_count":1},"private":"must-not-leak"}`)
	}, nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, adminTestRequest("GET", "settlement-health", ""))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"blocked_count":2`) || strings.Contains(recorder.Body.String(), "must-not-leak") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
	guard.err = errAdminForbidden
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, adminTestRequest("GET", "settlement-health", ""))
	if recorder.Code != 403 {
		t.Fatalf("revoked admin accepted: %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, adminTestRequest("PUT", "settlement-health", "{}"))
	if recorder.Code != 404 {
		t.Fatal("mutation route exposed")
	}
}

func TestAdminSettlementHealthMissingIsNotHealthy(t *testing.T) {
	for _, raw := range []string{`{}`, `{"observed_at":"2026-11-17T00:00:00Z"}`, `{"observed_at":"2026-11-17T00:00:00Z","snapshot":{}}`, `{"observed_at":"2026-11-17T00:00:00Z","snapshot":{"settlement_dead_count":-1,"settlement_retry_count":0}}`} {
		if _, err := adminSettlementHealthProjection([]byte(raw)); err == nil {
			t.Fatalf("accepted unknown health %s", raw)
		}
	}
}
