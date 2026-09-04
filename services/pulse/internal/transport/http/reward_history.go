package transporthttp

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type RewardHistoryReader interface {
	List(context.Context, uint64, int) ([]service.RewardHistoryItem, error)
}

// RewardHistoryRoute is read-only and derives user identity exclusively from
// the verified Principal. It never accepts a user_id path/query parameter.
func RewardHistoryRoute(router *gin.RouterGroup, reader RewardHistoryReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/me/rewards", auth, func(c *gin.Context) {
		principal, ok := PrincipalWithRole(c, "new-api")
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		limit := 0
		if raw := c.Query("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
			limit = parsed
		}
		items, err := reader.List(c.Request.Context(), principal.UserID, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "reward history unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"rewards": items})
	})
}
