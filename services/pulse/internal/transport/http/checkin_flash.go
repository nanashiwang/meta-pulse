package transporthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type checkinFlashService interface {
	GrantCheckinFlash(context.Context, uint64, service.CheckinFlash) (service.CheckinFlashResult, error)
	CheckinFlashStatus(context.Context, uint64, string) (service.CheckinFlashResult, error)
}

func CheckinFlashRoutes(r *gin.RouterGroup, s checkinFlashService, auth gin.HandlerFunc) {
	r.POST("/me/checkin-flash", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "community-bff")
		if !ok {
			c.Status(403)
			return
		}
		var body service.CheckinFlash
		if c.Request.URL.RawQuery != "" || bindSettingsJSON(c, &body) != nil {
			c.Status(400)
			return
		}
		out, err := s.GrantCheckinFlash(c.Request.Context(), p.UserID, body)
		if err != nil {
			status := 503
			if errors.Is(err, ports.ErrConflict) {
				status = 409
			}
			c.JSON(status, gin.H{"error": "checkin_flash_unavailable"})
			return
		}
		c.JSON(200, out)
	})
	r.GET("/me/checkin-flash/:id", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "community-bff")
		if !ok {
			c.Status(403)
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.Status(400)
			return
		}
		out, err := s.CheckinFlashStatus(c.Request.Context(), p.UserID, c.Param("id"))
		if err != nil {
			status := 503
			if errors.Is(err, ports.ErrConflict) || errors.Is(err, ports.ErrNotFound) {
				status = 404
			}
			c.JSON(status, gin.H{"error": "checkin_flash_unavailable"})
			return
		}
		c.JSON(200, out)
	})
}
