package pulse_user_center

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPollRoutesRejectBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, body, auth, origin string
		status                           int
	}{
		{"GET", "/metar/polls?question_id=1&question_id=2", "", "", "", 400},
		{"GET", "/metar/polls?question_id=../1", "", "", "", 400},
		{"GET", "/metar/polls?question_id=1&Authorization=secret", "", "", "", 400},
		{"GET", "/metar/polls/mine?question_id=1", "", "", "", 401},
		{"GET", "/metar/polls/voters?question_id=1", "", "", "", 401},
		{"GET", "/metar/polls/voters?question_id=1&user_id=2", "", "Bearer x", "", 400},
		{"GET", "/metar/polls/voters?question_id=1&after=1&after=2", "", "Bearer x", "", 400},
		{"GET", "/metar/polls/voters?question_id=1&after=../2", "", "Bearer x", "", 400},
		{"POST", "/metar/polls/command", `{"op":"close","question_id":"100"}`, "Bearer x", "https://evil.test", 403},
		{"POST", "/metar/polls/command", `{"op":"vote","question_id":"100","choices":[0],"user_id":"2"}`, "Bearer x", "https://forum.test", 400},
		{"POST", "/metar/polls/command", `{"op":"vote","question_id":"100","choices":[0],"choices":[1]}`, "Bearer x", "https://forum.test", 400},
	} {
		r := gin.New()
		uc := &UserCenter{}
		site := func() string { return "https://forum.test" }
		uc.registerPolls(r.Group(""), nil, site)
		uc.registerPolls(r.Group(""), func(*gin.Context) (string, error) { return "1", nil }, site)
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", tc.auth)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Metar-Request", "1")
		req.Header.Set("Idempotency-Key", "poll-test-request1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}
func TestPollIdentityFailure(t *testing.T) {
	r := gin.New()
	uc := &UserCenter{}
	uc.registerPolls(r.Group(""), func(*gin.Context) (string, error) { return "", errors.New("bad session") }, func() string { return "https://forum.test" })
	req := httptest.NewRequest("GET", "/metar/polls/mine?question_id=100", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("invalid identity accepted")
	}
}

func TestPollVotersInvalidSession(t *testing.T) {
	r := gin.New()
	uc := &UserCenter{}
	uc.registerPolls(r.Group(""), func(*gin.Context) (string, error) { return "", errors.New("bad session") }, func() string { return "https://forum.test" })
	req := httptest.NewRequest("GET", "/metar/polls/voters?question_id=100", nil)
	req.Header.Set("Authorization", "Bearer invalid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("invalid session accepted")
	}
	if w.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatal("private votes may be cached")
	}
}
