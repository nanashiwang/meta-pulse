package pulse_user_center

import (
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func newTestContext(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/callback"+rawQuery, nil)
	return ctx
}
