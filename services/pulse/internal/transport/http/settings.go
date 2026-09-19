package transporthttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
)

type RuntimeSettings interface {
	View(context.Context) (runtimeconfig.View, error)
	Update(context.Context, runtimeconfig.UpdateRequest, string, string) (runtimeconfig.View, error)
}

var settingsRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

func RuntimeSettingsRoutes(router *gin.RouterGroup, settings RuntimeSettings, auth gin.HandlerFunc) {
	if router == nil || settings == nil || auth == nil {
		return
	}
	admin := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		if _, ok := PrincipalWithRole(c, "admin"); !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		if c.Request.URL.RawQuery != "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		c.Next()
	}
	router.GET("/admin/settings", auth, admin, func(c *gin.Context) {
		view, err := settings.View(c.Request.Context())
		settingsResponse(c, view, err)
	})
	router.PUT("/admin/settings", auth, admin, func(c *gin.Context) {
		key := c.GetHeader("Idempotency-Key")
		if len(c.Request.Header.Values("Idempotency-Key")) != 1 || !settingsRequestID.MatchString(key) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_idempotency_key"})
			return
		}
		var request runtimeconfig.UpdateRequest
		if err := bindSettingsJSON(c, &request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		principal, _ := Principal(c)
		view, err := settings.Update(c.Request.Context(), request, strconv.FormatUint(principal.UserID, 10), key)
		settingsResponse(c, view, err)
	})
	router.POST("/admin/settings/secret", auth, admin, func(c *gin.Context) {
		var empty struct{}
		if err := bindSettingsJSON(c, &empty); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
			return
		}
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "settings_unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"secret": hex.EncodeToString(secret[:])})
	})
}

func settingsResponse(c *gin.Context, view runtimeconfig.View, err error) {
	if err == nil {
		c.JSON(http.StatusOK, view)
		return
	}
	switch {
	case errors.Is(err, runtimeconfig.ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_settings"})
	case errors.Is(err, runtimeconfig.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "settings_conflict"})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "settings_unavailable"})
	}
}

func bindSettingsJSON(c *gin.Context, target any) error {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" || c.Request.Body == nil {
		return errors.New("JSON required")
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 32769))
	if err != nil || len(data) > 32768 {
		return errors.New("invalid body")
	}
	check := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSONValue(check, 0); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// Duplicate names are rejected at every nesting level, including the secret
// map, rather than letting the last authenticated spelling silently win.
func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return errors.New("JSON too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		if depth == 0 || token == nil {
			return errors.New("null or non-object JSON")
		}
		return nil
	}
	if depth == 0 && delim != '{' {
		return errors.New("object required")
	}
	if delim != '{' && delim != '[' {
		return errors.New("invalid delimiter")
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return errors.New("duplicate JSON key")
			}
			seen[name] = true
		}
		if err := uniqueJSONValue(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
