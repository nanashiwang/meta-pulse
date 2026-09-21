package transporthttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
	"net/http"
)

type RewardRulesReader interface {
	Get(context.Context) (service.RewardRules, error)
}

func RewardRulesRoute(router *gin.RouterGroup, reader RewardRulesReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	router.GET("/me/rules", auth, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if _, ok := ProductPrincipal(c); !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid query"})
			return
		}
		var result service.RewardRules
		var err error
		if personal, ok := reader.(interface {
			GetForUser(context.Context, uint64) (service.RewardRules, error)
		}); ok {
			principal, _ := ProductPrincipal(c)
			result, err = personal.GetForUser(c.Request.Context(), principal.UserID)
		} else {
			result, err = reader.Get(c.Request.Context())
		}
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rules unavailable"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
