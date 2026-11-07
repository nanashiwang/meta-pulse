package middleware

import (
	"context"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/siteinfo_common"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

type metarSite struct {
	siteinfo_common.SiteInfoCommonService
}

func (metarSite) GetSiteGeneral(context.Context) (*schema.SiteGeneralResp, error) {
	return &schema.SiteGeneralResp{SiteUrl: "https://community.example"}, nil
}
func TestMetarCookieRenewal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	am := &AuthUserMiddleware{siteInfoCommonService: metarSite{}}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest("GET", "https://community.example/answer/api/v1/question/page", nil)
		ctx.Request.Header.Set("Cookie", "visit=existing")
		am.renewVisitCookie(ctx, &entity.UserCacheInfo{UserStatus: 1, VisitToken: "existing"})
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("existing cookie not renewed")
		}
		c := cookies[0]
		if c.MaxAge != 2592000 || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Value != "existing" {
			t.Fatal("invalid cookie policy")
		}
	}
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	am.renewVisitCookie(ctx, &entity.UserCacheInfo{UserStatus: 9, VisitToken: "suspended"})
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("suspended cookie renewed")
	}
}
