package pulse_user_center

import (
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/events"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventBoundaryRejectsSpoofingAndWrongRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc := &UserCenter{}
	for _, tc := range []struct {
		raw, token, origin, query string
		admin                     bool
		status                    int
	}{
		{`{"op":"join","event":{"id":"1"}}`, "", "https://metarhub.com", "", false, 401},
		{`{"op":"join","event":{"id":"1"}}`, "test", "https://evil.invalid", "", false, 403},
		{`{"op":"join","event":{"id":"1"}}`, "test", "https://metarhub.com", "?user_id=3", false, 400},
		{`{"op":"create","event":{}}`, "test", "https://metarhub.com", "", false, 403},
		{`{"op":"join","event":{"id":"1"}}`, "test", "https://metarhub.com", "", true, 403},
		{`{"op":"join","user_id":"3","event":{"id":"1"}}`, "test", "https://metarhub.com", "", false, 400},
		{`{"op":"join","op":"leave","event":{"id":"1"}}`, "test", "https://metarhub.com", "", false, 400},
	} {
		r := gin.New()
		r.POST("/command", uc.eventHandler("command", func(*gin.Context) (string, error) { return "2", nil }, tc.admin, func() string { return "https://metarhub.com" }))
		req := httptest.NewRequest("POST", "/command"+tc.query, strings.NewReader(tc.raw))
		req.Header.Set("Authorization", tc.token)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Idempotency-Key", "request-key")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s got %d", tc.raw, w.Code)
		}
	}
	var c events.Command
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"op":"create","event":{"title":"test"}} {}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if chatJSON(ctx, &c) {
		t.Fatal("trailing object accepted")
	}
}

func TestRepliedThreadsMustDeriveUserFromSession(t *testing.T) {
	router := gin.New()
	router.GET("/replies", (&UserCenter{}).eventHandler("replied_threads", func(*gin.Context) (string, error) { return "2", nil }, false, func() string { return "" }))
	for _, tc := range []struct {
		path, auth string
		status     int
	}{{"/replies", "", 401}, {"/replies?user_id=9", "session", 400}, {"/replies?page=1&page=2", "session", 400}} {
		req := httptest.NewRequest("GET", tc.path, nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != tc.status {
			t.Fatalf("%s: %d", tc.path, out.Code)
		}
	}
}
