// Package transporthttp contains HTTP-only adapters. Business rules stay in
// service/domain packages.
package transporthttp

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/security"
)

const PrincipalContextKey = "pulse.principal"

type SecretResolver func(role string) []byte

// SignedRequest authenticates requests from new-api BFF, Worker, and other
// explicitly configured services. A missing or unknown role fails closed.
func SignedRequest(resolve SecretResolver, nonces security.NonceStore, allowedSkew time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetHeader(security.HeaderRole)
		var secret []byte
		if resolve != nil {
			secret = resolve(role)
		}
		principal, err := security.VerifyRequest(c.Request, secret, time.Now(), allowedSkew, nonces)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(PrincipalContextKey, principal)
		c.Next()
	}
}

func Principal(c *gin.Context) (security.Principal, bool) {
	value, exists := c.Get(PrincipalContextKey)
	if !exists {
		return security.Principal{}, false
	}
	principal, ok := value.(security.Principal)
	return principal, ok
}
