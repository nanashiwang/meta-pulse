package pulse_user_center

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAwardsRejectUnsafeRequestsBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, body, auth, origin string
		status                           int
	}{
		{"GET", "/public/metar/awards?type=week&key=2026-11-17&kind=post", "", "", "", 400},
		{"GET", "/public/metar/awards?type=week&key=2026-11-16&kind=post&admin=true", "", "", "", 400},
		{"GET", "/public/metar/awards?type=week&kind=post&page=0", "", "", "", 400},
		{"GET", "/public/metar/awards?type=month&kind=unknown", "", "", "", 400},
		{"GET", "/public/metar/awards?type=week&key=&kind=post", "", "", "", 400},
		{"GET", "/admin/metar/awards?type=week&kind=post", "", "Bearer x", "", 400},
		{"GET", "/member/metar/awards/honors", "", "", "", 401},
		{"POST", "/member/metar/awards/command", `{"op":"paid","period":{"type":"week","key":"2026-11-09","kind":"post"},"subject_id":"100","reason":"fake"}`, "Bearer x", "https://forum.test", 403},
		{"POST", "/member/metar/awards/command", `{"op":"grant","period":{"type":"week","key":"2026-11-09","kind":"post"},"subject_id":"100"}`, "Bearer x", "https://forum.test", 403},
		{"POST", "/admin/metar/awards/command", `{"op":"grant","period":{"type":"week","key":"2026-11-09","kind":"post"},"subject_id":"100","amount":1000}`, "Bearer x", "https://forum.test", 400},
		{"POST", "/admin/metar/awards/command", `{"op":"rec_grant","period":{"type":"week","key":"2026-11-09","kind":"post"},"subject_id":"100","recommender_id":"4","external_id":"7"}`, "Bearer x", "https://forum.test", 400},
		{"GET", "/member/metar/awards/status?question_id=1", "", "", "", 401},
		{"POST", "/member/metar/awards/command", `{"op":"publish","period":{"type":"week","key":"2026-11-09","kind":"post"}}`, "Bearer x", "https://forum.test", 403},
		{"POST", "/admin/metar/awards/command", `{"op":"sync","period":{"type":"week","key":"2026-11-09","kind":"post"}}`, "Bearer x", "https://evil.test", 403},
		{"POST", "/admin/metar/awards/command", `{"op":"sync","period":{"type":"week","key":"2026-11-09","kind":"post"},"user_id":"2"}`, "Bearer x", "https://forum.test", 400},
		{"POST", "/admin/metar/awards/command", `{"op":"sync","op":"publish"}`, "Bearer x", "https://forum.test", 400},
	} {
		r := gin.New()
		uc := &UserCenter{}
		site := func() string { return "https://forum.test" }
		session := func(*gin.Context) (string, error) { return "3", nil }
		uc.registerAwards(r.Group("/public"), nil, false, site)
		uc.registerAwards(r.Group("/member"), session, false, site)
		uc.registerAwards(r.Group("/admin"), session, true, site)
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", tc.auth)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Idempotency-Key", "awards-test-00001")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s got %d: %s", tc.path, w.Code, w.Body)
		}
	}
}
