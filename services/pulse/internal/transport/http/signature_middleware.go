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
type SecretSetResolver func(role string) [][]byte

// SignedRequest authenticates calls from new-api BFF, Worker, and other
// explicitly configured services. A missing or unknown role fails closed.
// It remains as a single-secret compatibility wrapper; new deployments that
// rotate keys should use SignedRequestWithSecrets.
func SignedRequest(resolve SecretResolver, nonces security.NonceStore, allowedSkew time.Duration) gin.HandlerFunc {
	return SignedRequestWithSecrets(func(role string) [][]byte {
		if resolve == nil {
			return nil
		}
		return [][]byte{resolve(role)}
	}, nonces, allowedSkew)
}

// SignedRequestWithSecrets accepts the active secret followed by an optional
// previous secret. Outbound callers continue signing with the active key,
// while this boundary can safely drain requests signed before rotation.
func SignedRequestWithSecrets(resolve SecretSetResolver, nonces security.NonceStore, allowedSkew time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetHeader(security.HeaderRole)
		var secrets [][]byte
		if resolve != nil {
			secrets = resolve(role)
		}
		principal, err := security.VerifyRequestWithSecrets(c.Request, secrets, time.Now(), allowedSkew, nonces)
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

func PrincipalWithRole(c *gin.Context, role string) (security.Principal, bool) {
	principal, ok := Principal(c)
	return principal, ok && principal.UserID != 0 && principal.Role == role
}
