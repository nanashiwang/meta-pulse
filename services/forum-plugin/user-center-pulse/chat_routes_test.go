package pulse_user_center

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/chat"
)

func TestChatJSONStrictAndBounded(t *testing.T) {
	for _, raw := range []string{`{"op":"send","op":"read"}`, `{"op":"send","user_id":"2"}`, `{} {}`, `[]`, `{"members":["2"],"members":["3"]}`, `{"body":"` + strings.Repeat("x", 32769) + `"}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(raw))
		c.Request.Header.Set("Content-Type", "application/json")
		var cmd chat.Command
		if chatJSON(c, &cmd) {
			t.Fatal("accepted ambiguous/oversized payload")
		}
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"op":"send","room_id":"3","body":"你好 🙂"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	var cmd chat.Command
	if !chatJSON(c, &cmd) {
		t.Fatal("valid text rejected")
	}
}
func TestChatIdentityOriginAndQueryFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc := &UserCenter{}
	session := func(*gin.Context) (string, error) { return "2", nil }
	for _, tc := range []struct {
		path, token, origin string
		status              int
	}{
		{"/command", "", "https://metar.uk", 401}, {"/command", "test", "https://evil.invalid", 403}, {"/command?user_id=3", "test", "https://metar.uk", 400}, {"/command?token=secret", "test", "https://metar.uk", 400},
	} {
		r := gin.New()
		r.POST("/command", uc.chatHandler("command", session, false, func() string { return "https://metar.uk" }))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", tc.path, bytes.NewBufferString("{}"))
		req.Header.Set("Authorization", tc.token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Metar-Request", "1")
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("private response cached")
		}
	}
	r := gin.New()
	r.GET("/summary", uc.chatHandler("summary", func(*gin.Context) (string, error) { return "", errors.New("identity unavailable") }, false, func() string { return "https://metar.uk" }))
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/summary", nil)
	req.Header.Set("Authorization", "test")
	r.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
