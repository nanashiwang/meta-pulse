package pulse_user_center

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFarmExchangeRequiresPaidOrderForSameBinding(t *testing.T) {
	const order = "0123456789abcdef0123456789abcdef"
	const proof = "ae8a3e90170568e95aded33dc6296ed863db0b56dc45e14ddf29cc74d2b7f405"
	const body = `{"order_id":"0123456789abcdef0123456789abcdef"}`
	calls := 0
	router, _, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/internal/me/farm-ticket-exchanges" || r.Header.Get("X-Farm-Receipt") != "" || r.Header.Get(pulseHeaderUserID) != "42" {
			t.Error("unexpected forwarding")
		}
		w.Write([]byte(`{"entry_id":"17","order_id":"0123456789abcdef0123456789abcdef"}`))
	})
	test := func(sig string, want int) {
		req := communityTestRequest("POST", "farm-exchanges", body)
		req.Header.Set("Idempotency-Key", order)
		req.Header.Set("X-Farm-Receipt", sig)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("status %d want %d", w.Code, want)
		}
	}
	t.Setenv("TOKEN_FARM_EXCHANGE_KEY", "")
	test(proof, 403)
	t.Setenv("TOKEN_FARM_EXCHANGE_KEY", strings.Repeat("01", 32))
	test("", 403)
	guard.identity = "43"
	test(proof, 403)
	guard.identity = "42"
	test(proof, 200)
	if calls != 1 {
		t.Fatal(calls)
	}
}
