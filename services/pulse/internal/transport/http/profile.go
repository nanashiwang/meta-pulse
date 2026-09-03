package transporthttp

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type ProfileReader interface {
	Get(context.Context, uint64) (service.UserProfile, error)
}

// ProfileRoute is intentionally read-only. Authentication is supplied by the
// caller so BFF and forum roles can use their own secrets.
func ProfileRoute(router *gin.RouterGroup, reader ProfileReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/users/:user_id/profile", auth, func(c *gin.Context) {
		userID, err := strconv.ParseUint(c.Param("user_id"), 10, 64)
		if err != nil || userID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
			return
		}
		profile, err := reader.Get(c.Request.Context(), userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "profile unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id":                     profile.UserID,
			"lifetime_contribution_milli": profile.LifetimeContribution,
			"level":                       gin.H{"key": profile.Level.Key, "name": profile.Level.Name},
		})
	})
}
