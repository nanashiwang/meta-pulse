package transporthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type CommunityTicketGranter interface {
	Grant(context.Context, uint64, service.CommunityTicket) (service.CommunityTicketResult, error)
}

func CommunityTicketRoute(r *gin.RouterGroup, s CommunityTicketGranter, auth gin.HandlerFunc) {
	r.POST("/me/community-tickets", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "community-bff")
		if !ok {
			c.JSON(403, gin.H{"error": "forbidden"})
			return
		}
		var task service.CommunityTicket
		if c.Request.URL.RawQuery != "" || bindSettingsJSON(c, &task) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		result, err := s.Grant(c.Request.Context(), p.UserID, task)
		if err != nil {
			status := 503
			if errors.Is(err, ports.ErrConflict) {
				status = 409
			}
			c.JSON(status, gin.H{"error": "community_ticket_unavailable"})
			return
		}
		c.JSON(200, result)
	})
}
