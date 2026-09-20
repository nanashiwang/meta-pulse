package transporthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"strconv"
)

type ExperienceDeliverer interface {
	Pending(context.Context, uint64) ([]service.ExperienceDelivery, error)
	Acknowledge(context.Context, uint64, string, string) error
	Reverse(context.Context, string, string, string, string) error
}

func ExperienceRoutes(r *gin.RouterGroup, s ExperienceDeliverer, auth gin.HandlerFunc) {
	if r == nil || s == nil || auth == nil {
		return
	}
	guard := func(role string) gin.HandlerFunc {
		return func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			if _, ok := PrincipalWithRole(c, role); !ok {
				c.AbortWithStatusJSON(403, gin.H{"error": "forbidden"})
				return
			}
			if c.Request.URL.RawQuery != "" {
				c.AbortWithStatusJSON(400, gin.H{"error": "invalid_request"})
				return
			}
		}
	}
	respond := func(c *gin.Context, err error) {
		status := 503
		if errors.Is(err, ports.ErrConflict) {
			status = 409
		}
		if errors.Is(err, ports.ErrNotFound) {
			status = 404
		}
		c.JSON(status, gin.H{"error": "experience_delivery_unavailable"})
	}
	r.GET("/me/experience", auth, guard("community-bff"), func(c *gin.Context) {
		p, _ := Principal(c)
		items, err := s.Pending(c.Request.Context(), p.UserID)
		if err != nil {
			respond(c, err)
			return
		}
		c.JSON(200, gin.H{"deliveries": items})
	})
	r.POST("/me/experience/ack", auth, guard("community-bff"), func(c *gin.Context) {
		var body struct {
			GrantID string `json:"grant_id"`
			Status  string `json:"status"`
		}
		if bindSettingsJSON(c, &body) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		p, _ := Principal(c)
		if err := s.Acknowledge(c.Request.Context(), p.UserID, body.GrantID, body.Status); err != nil {
			respond(c, err)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
	r.POST("/admin/experience/reverse", auth, guard("admin"), func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		var body struct {
			GrantID string `json:"grant_id"`
			Reason  string `json:"reason"`
		}
		if len(c.Request.Header.Values("Idempotency-Key")) != 1 || !settingsRequestID.MatchString(key) || bindSettingsJSON(c, &body) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		p, _ := Principal(c)
		if err := s.Reverse(c.Request.Context(), strconv.FormatUint(p.UserID, 10), key, body.GrantID, body.Reason); err != nil {
			respond(c, err)
			return
		}
		c.JSON(200, gin.H{"ok": true})
	})
}
