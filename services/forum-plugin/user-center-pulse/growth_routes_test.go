package pulse_user_center

import (
	"bytes"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestExperienceJSONRejectsAmbiguousPayloads(t *testing.T) {
	for _, raw := range []string{`{"rules":{"amount":1,"amount":2}}`, `{"x":1,"x":2}`, `{} {}`, `[]`, `{"user_id":"1"}`, `{"a":{}}`} {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		var body struct{}
		if experienceJSON(c, &body) {
			t.Fatal("accepted", raw)
		}
	}
	if !uniqueExperienceJSON([]byte(`{"rules":{"amount":1},"items":[1,2]}`)) {
		t.Fatal("valid nested JSON")
	}
}
func TestExperienceIdentityAndOriginAreCheckedBeforeStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc := &UserCenter{}
	session := func(*gin.Context) (string, error) { return "2", nil }
	for _, tc := range []struct {
		url, token, origin string
		code               int
	}{{"/checkin?user_id=3", "ok", "https://metar.uk", 400}, {"/checkin?Authorization=leak", "", "https://metar.uk", 400}, {"/checkin?a=1;b=2", "ok", "https://metar.uk", 400}, {"/checkin", "", "https://metar.uk", 401}, {"/checkin", "ok", "https://evil.invalid", 403}} {
		r := gin.New()
		r.POST("/checkin", uc.experienceHandler("checkin", session, false, func() string { return "https://metar.uk" }))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", tc.url, bytes.NewBufferString("{}"))
		req.Header.Set("Authorization", tc.token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Metar-Request", "1")
		r.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("%s %d want%d", tc.url, w.Code, tc.code)
		}
	}
	r := gin.New()
	r.GET("/summary", uc.experienceHandler("summary", func(*gin.Context) (string, error) { return "", errors.New("session unavailable") }, false, func() string { return "https://metar.uk" }))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/summary", nil)
	req.Header.Set("Authorization", "ok")
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("failed session succeeded")
	}
}
