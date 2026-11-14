package pulse_user_center

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestPublicRankingInvalidQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "kind=invalid", "kind=posts&kind=answers", "kind=level&user_id=1", "kind=%zz"} {
		r := gin.New()
		uc := &UserCenter{}
		uc.registerPublicExperience(r.Group("/answer/api/v1"))
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/answer/api/v1/metar/ranking?"+query, nil)
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("query %q status %d", query, w.Code)
		}
	}
}
