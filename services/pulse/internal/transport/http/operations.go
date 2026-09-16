package transporthttp

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/service"
)

type OperationsOverviewReader interface {
	Load(context.Context) (service.OperationsOverview, error)
}

// OperationsOverviewRoute exposes the read-only operations console projection.
// It is restricted to the admin role and has no mutating verb: the console
// reads state here and performs every change through the audited paths.
func OperationsOverviewRoute(router *gin.RouterGroup, reader OperationsOverviewReader, auth gin.HandlerFunc) {
	if router == nil || reader == nil || auth == nil {
		return
	}
	admin := func(c *gin.Context) {
		principal, ok := Principal(c)
		if !ok || principal.UserID == 0 || principal.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}
		c.Next()
	}
	router.GET("/admin/operations/overview", auth, admin, func(c *gin.Context) {
		overview, err := reader.Load(c.Request.Context())
		if err != nil {
			// The error text can name internal tables; the console only needs
			// to know the projection is unavailable.
			c.JSON(http.StatusInternalServerError, gin.H{"error": "operations overview unavailable"})
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, overview)
	})
}
