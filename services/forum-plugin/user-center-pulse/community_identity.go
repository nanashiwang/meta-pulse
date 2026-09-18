package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
)

var (
	errCommunityUnauthenticated    = errors.New("authentication_required")
	errCommunityAccountUnavailable = errors.New("account_unavailable")
	errCommunityBindingRequired    = errors.New("binding_required")
)

// Answer v1.7.1 exposes authenticated plugin routers but no public session
// accessor. Read only the exact server-created cache type behind its middleware
// key. Never deserialize browser data into this identity. A changed Answer
// contract fails closed until this adapter and its compatibility test are updated.
func answerSessionUserID(c *gin.Context) (string, error) {
	if c == nil {
		return "", errCommunityUnauthenticated
	}
	value, ok := c.Get("ctxUuidKey")
	if !ok {
		return "", errCommunityUnauthenticated
	}
	return answerSessionValueUserID(value)
}

func answerSessionValueUserID(value any) (string, error) {
	v := reflect.ValueOf(value)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return "", errCommunityUnauthenticated
	}
	t := v.Type().Elem()
	if t.Kind() != reflect.Struct || t.PkgPath() != "github.com/apache/answer/internal/entity" || t.Name() != "UserCacheInfo" {
		return "", errCommunityUnauthenticated
	}
	v = v.Elem()
	user, status, email := v.FieldByName("UserID"), v.FieldByName("UserStatus"), v.FieldByName("EmailStatus")
	if !user.IsValid() || user.Kind() != reflect.String || !status.IsValid() || status.Kind() != reflect.Int || !email.IsValid() || email.Kind() != reflect.Int {
		return "", errCommunityUnauthenticated
	}
	if !canonicalCommunityID(user.String()) {
		return "", errCommunityUnauthenticated
	}
	if status.Int() != 1 || email.Int() != 1 {
		return "", errCommunityAccountUnavailable
	}
	return user.String(), nil
}

func canonicalCommunityID(value string) bool {
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == value
}

type communityIdentityReader interface {
	CommunityIdentity(context.Context, string) (string, error)
}

// CommunityIdentity rechecks live Answer account status and the immutable
// one-to-one relation. Session caches and their ExternalID are not binding facts.
func (g *mysqlBindingGuard) CommunityIdentity(ctx context.Context, forumID string) (string, error) {
	if !canonicalCommunityID(forumID) {
		return "", errCommunityUnauthenticated
	}
	if err := g.Ready(ctx); err != nil {
		return "", err
	}
	rows, err := g.db.QueryContext(ctx, "SELECT u.status, u.mail_status, b.external_id FROM `user` u LEFT JOIN user_external_login b ON b.user_id = u.id AND b.provider = ? WHERE u.id = ? LIMIT 2", pluginSlug, forumID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", errCommunityAccountUnavailable
	}
	var status, email int
	var externalID sql.NullString
	if err := rows.Scan(&status, &email, &externalID); err != nil {
		return "", err
	}
	if rows.Next() {
		return "", errors.New("ambiguous community binding")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if status != 1 || email != 1 {
		return "", errCommunityAccountUnavailable
	}
	if !externalID.Valid {
		return "", errCommunityBindingRequired
	}
	if !canonicalCommunityID(externalID.String) {
		return "", errors.New("invalid community binding")
	}
	return externalID.String, nil
}
