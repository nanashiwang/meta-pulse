package pulse_user_center

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGiftMemberRefusesSpoofedCriteriaAndSession(t *testing.T) {
	u := &UserCenter{}
	r := gin.New()
	r.POST("/gift", u.giftMember(func(*gin.Context) (string, error) { return "2", nil }, func() string { return "https://forum.test" }, true))
	for _, v := range []struct {
		body, token, origin string
		status              int
	}{{`{"id":"1"}`, "", "https://forum.test", 401}, {`{"id":"1","flash":100}`, "test", "https://forum.test", 400}, {`{"id":"1","user_id":"3"}`, "test", "https://forum.test", 400}, {`{"id":"1"}`, "test", "https://other.test", 400}} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/gift", strings.NewReader(v.body))
		req.Header.Set("Authorization", v.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", v.origin)
		req.Header.Set("X-Metar-Request", "1")
		r.ServeHTTP(w, req)
		if w.Code != v.status {
			t.Fatal(w.Code)
		}
	}
}
