package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/gin-gonic/gin"
)

var errAdminForbidden = errors.New("forbidden")

const answerAdminRoleID = 2

// Answer's AdminAuth relies on its admin-token cache without rechecking roles.
// Settings require both an explicit admin session and a fresh DB check, so a
// cached identity cannot survive demotion, suspension or loss of activation.
func answerAdminSessionUserID(c *gin.Context) (string, error) {
	id, err := answerSessionUserID(c)
	if err != nil {
		return "", err
	}
	value, _ := c.Get("ctxUuidKey")
	role := reflect.ValueOf(value).Elem().FieldByName("RoleID")
	if !role.IsValid() || role.Kind() != reflect.Int || role.Int() != answerAdminRoleID {
		return "", errAdminForbidden
	}
	return id, nil
}

type adminIdentityReader interface {
	CheckAdminIdentity(context.Context, string) error
}

func (g *mysqlBindingGuard) CheckAdminIdentity(ctx context.Context, forumID string) error {
	if !canonicalCommunityID(forumID) {
		return errCommunityUnauthenticated
	}
	// Admin settings do not require a new-api account binding. A malformed or
	// ambiguous role relation fails closed, including stale cached admins.
	rows, err := g.db.QueryContext(ctx, "SELECT u.status, u.mail_status, r.role_id FROM `user` u LEFT JOIN user_role_rel r ON r.user_id = u.id WHERE u.id = ? LIMIT 2", forumID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errAdminForbidden
	}
	var status, email int
	var role sql.NullInt64
	if err := rows.Scan(&status, &email, &role); err != nil {
		return err
	}
	if rows.Next() {
		return errAdminForbidden
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if status != 1 || email != 1 || !role.Valid || role.Int64 != answerAdminRoleID {
		return errAdminForbidden
	}
	return nil
}
