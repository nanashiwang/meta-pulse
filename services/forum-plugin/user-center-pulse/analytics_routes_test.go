package pulse_user_center

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOperationsRejectsIdentityForgeryAndCrossSite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	uc := &UserCenter{}
	site := func() string { return "https://metarhub.com" }
	for _, tc := range []struct {
		body, origin, token, query string
		auth                       bool
		status                     int
	}{
		{`{"kind":"visit","user_id":"1"}`, site(), "", "", false, 400},
		{`{"kind":"read","kind":"visit"}`, site(), "", "", false, 400},
		{`{"kind":"visit","source":"https://private.invalid/path?token=x"}`, site(), "", "", false, 400},
		{`{"kind":"visit"}`, "https://evil.invalid", "", "", false, 403},
		{`{"kind":"visit"}`, site(), "", "?user_id=1", false, 403},
		{`{"kind":"visit"}`, site(), "fake", "", false, 400},
		{`{"kind":"visit"}`, site(), "", "", true, 401},
	} {
		var session func(*gin.Context) (string, error)
		if tc.auth {
			session = func(*gin.Context) (string, error) { t.Fatal("unexpected session lookup"); return "", nil }
		}
		r := gin.New()
		r.POST("/track", uc.operationsVisit(session, site))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/track"+tc.query, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Authorization", tc.token)
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache")
		}
	}
	if uc.opsStore != nil {
		t.Fatal("unauthorized schema initialization")
	}
}
func TestOperationsAdminDeniedBeforeDatabase(t *testing.T) {
	uc := &UserCenter{}
	site := func() string { return "https://metarhub.com" }
	session := func(*gin.Context) (string, error) { return "", errors.New("identity unavailable") }
	for _, token := range []string{"", "test"} {
		r := gin.New()
		r.GET("/report", uc.operationsAdmin("report", session, site))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/report", nil)
		req.Header.Set("Authorization", token)
		r.ServeHTTP(w, req)
		want := 401
		if token != "" {
			want = 503
		}
		if w.Code != want {
			t.Fatal(w.Code)
		}
	}
}
