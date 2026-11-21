package pulse_user_center

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFarmExchangeRequiresPaidOrderForSameBinding(t *testing.T) {
	const order = "0123456789abcdef0123456789abcdef"
	const proof = "v2.06b05d5902da8b058ec1c44ce541f7f50ed12c36402a0d99ef9dc041a418e56f.38c9b2590880bcb88fcdbb20cd31593719026f3d3e1a4fb2e2fdab084f1b14a3339c4822431a8451c657306fa73949c6cd92bd5dd93794b24603cfe95ecae504"
	const testPin = "a840172a6458f1fe0a1e7946a82d0afe549e827933360f0d4841dda67c63d8ed"
	const body = `{"order_id":"0123456789abcdef0123456789abcdef"}`
	calls := 0
	router, uc, guard := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
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
	// Non-production signer cannot issue tickets via the production router.
	test(proof, 403)
	router = gin.New()
	router.POST("/answer/api/v1/metar/pulse/farm-exchanges", uc.communityHandlerWithPin("farm-exchanges", func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" }, testPin))
	test("", 403)
	guard.identity = "43"
	test(proof, 403)
	guard.identity = "42"
	test(proof, 200)
	if calls != 1 {
		t.Fatal(calls)
	}
}
