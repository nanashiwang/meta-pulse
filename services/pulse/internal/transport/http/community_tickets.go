package transporthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/domain/ledger"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"strconv"
)

type CommunityTicketGranter interface {
	Grant(context.Context, uint64, service.CommunityTicket) (service.CommunityTicketResult, error)
	AdminGrant(context.Context, string, string, service.AdminTicketGrant) (service.AdminTicketGrantResult, error)
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
	r.POST("/admin/tickets/statistics", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := PrincipalWithRole(c, "admin"); !ok {
			c.Status(403)
			return
		}
		var body struct {
			UserIDs []uint64 `json:"user_ids"`
		}
		if c.Request.URL.RawQuery != "" || bindSettingsJSON(c, &body) != nil || len(body.UserIDs) == 0 || len(body.UserIDs) > 100 {
			c.Status(400)
			return
		}
		for _, id := range body.UserIDs {
			if id == 0 {
				c.Status(400)
				return
			}
		}
		reader, ok := s.(interface {
			Statistics(context.Context, []uint64) (map[uint64]ports.TicketStatistics, error)
		})
		if !ok {
			c.Status(503)
			return
		}
		result, err := reader.Statistics(c.Request.Context(), body.UserIDs)
		if err != nil {
			c.Status(503)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/admin/tickets/grant", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		p, ok := PrincipalWithRole(c, "admin")
		if !ok {
			c.JSON(403, gin.H{"error": "forbidden"})
			return
		}
		key := c.GetHeader("Idempotency-Key")
		var grant service.AdminTicketGrant
		if c.Request.URL.RawQuery != "" || len(c.Request.Header.Values("Idempotency-Key")) != 1 || !settingsRequestID.MatchString(key) || bindSettingsJSON(c, &grant) != nil {
			c.JSON(400, gin.H{"error": "invalid_request"})
			return
		}
		result, err := s.AdminGrant(c.Request.Context(), strconv.FormatUint(p.UserID, 10), key, grant)
		switch {
		case err == nil:
			c.JSON(200, result)
		case errors.Is(err, service.ErrInvalidAdminTicketGrant):
			c.JSON(400, gin.H{"error": "invalid_request"})
		case errors.Is(err, ledger.ErrIdempotencyConflict), errors.Is(err, ports.ErrConflict):
			c.JSON(409, gin.H{"error": "ticket_grant_conflict"})
		default:
			c.JSON(503, gin.H{"error": "ticket_grant_unavailable"})
		}
	})
}
