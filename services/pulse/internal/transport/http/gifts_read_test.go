package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"net/http/httptest"

	"testing"
)

type giftCapture struct{ called bool }

func (g *giftCapture) QueueGift(context.Context, uint64, service.GiftOrder) error {
	g.called = true
	return nil
}
func (*giftCapture) GiftUsage(context.Context, uint64) (string, *int64, bool, error) {
	return "", nil, false, nil
}
func (*giftCapture) GiftPaid(context.Context, uint64, uint64) (bool, int64, error) {
	return false, 0, nil
}
func TestGiftStatusRejectsWrongPrincipal(t *testing.T) {
	for _, role := range []string{"admin", "community-bff"} {
		g := &giftCapture{}
		r := gin.New()
		giftReadRoutes(r.Group("/"), g, func(c *gin.Context) { c.Set(PrincipalContextKey, security.Principal{UserID: 42, Role: role}) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/me/gift-status/7", nil))
		if (w.Code == 200) != (role == "community-bff") || g.called {
			t.Fatal(w.Code)
		}
	}
}
