package transporthttp

import (
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"strconv"
)

func giftReadRoutes(r *gin.RouterGroup, g service.GiftProjection, auth gin.HandlerFunc) {
	r.GET("/me/gift-status/:round", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "community-bff")
		id, e := strconv.ParseUint(c.Param("round"), 10, 64)
		if !ok || e != nil || id == 0 || c.Request.URL.RawQuery != "" {
			c.Status(403)
			return
		}
		paid, tickets, e := g.GiftPaid(c.Request.Context(), p.UserID, id)
		if e != nil {
			c.Status(503)
			return
		}
		c.JSON(200, gin.H{"flash_paid": paid, "tickets": tickets})
	})
	r.GET("/me/gift-usage", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "community-bff")
		if !ok || c.Request.URL.RawQuery != "" {
			c.Status(403)
			return
		}
		day, q, ready, e := g.GiftUsage(c.Request.Context(), p.UserID)
		if e != nil || !ready {
			c.Status(503)
			return
		}
		c.JSON(200, gin.H{"Day": day, "Quota": q})
	})
}
