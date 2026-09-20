package pulse_user_center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const adminPeriodBody = `{"key":"period-1","starts_at":"2030-01-01T00:00:00+08:00","multiplier_bps":12500,"ticket_threshold_milli":1500000,"reward_budget":1000,"rewards":[{"key":"prize","amount":10,"weight":1}],"reason":"reviewed pool"}`

func TestAdminPeriodBFF(t *testing.T) {
	calls := 0
	router, _, guard := adminTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/internal/admin/periods" || r.Header.Get(pulseHeaderUserID) != "7" || r.Header.Get("Idempotency-Key") != "request_1" || r.Header.Get("Authorization") != "" {
			t.Error("invalid admin projection")
		}
		w.Write([]byte(`{"period_id":1,"period_key":"period-1","status":"active","ticket_threshold_milli":1500000,"starts_at":"2030-01-01T00:00:00Z","ends_at":"2030-01-11T00:00:00Z","secret":"must-not-leak"}`))
	}, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, adminTestRequest("PUT", "periods", adminPeriodBody))
	if w.Code != 200 || strings.Contains(w.Body.String(), "must-not-leak") || calls != 1 {
		t.Fatalf("unexpected response %d %s", w.Code, w.Body)
	}
	for _, body := range []string{strings.Replace(adminPeriodBody, `"reason":`, `"actor_id":"8","reason":`, 1), strings.Replace(adminPeriodBody, `"weight":1`, `"weight":1,"weight":2`, 1)} {
		w = httptest.NewRecorder()
		router.ServeHTTP(w, adminTestRequest("PUT", "periods", body))
		if w.Code != 400 || calls != 1 {
			t.Fatal("unsafe input forwarded")
		}
	}
	guard.err = errAdminForbidden
	w = httptest.NewRecorder()
	router.ServeHTTP(w, adminTestRequest("PUT", "periods", adminPeriodBody))
	if w.Code != 403 || calls != 1 {
		t.Fatal("non-admin forwarded")
	}
}
