package transporthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type RewardFeedReader interface {
	Recent(context.Context) (service.RewardFeed, error)
}

type AdminRewardFeedReader interface {
	RecentForAdmin(context.Context) (service.AdminRewardFeed, error)
}

func RewardFeedRoute(router *gin.RouterGroup, reader RewardFeedReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/community/rewards", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := PrincipalWithRole(c, "community-bff"); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		result, err := reader.Recent(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reward_feed_unavailable"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
}

func AdminRewardFeedRoute(router *gin.RouterGroup, reader AdminRewardFeedReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/admin/community/rewards", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := PrincipalWithRole(c, "admin"); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		result, err := reader.RecentForAdmin(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "reward_feed_unavailable"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
