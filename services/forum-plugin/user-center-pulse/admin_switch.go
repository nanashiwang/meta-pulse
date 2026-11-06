package pulse_user_center

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
)

const switchSlug = "metar_admin_switch"
const switchCallback = "/answer/api/v1/connector/redirect/" + switchSlug

var switchProof = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errSwitchDenied = errors.New("account switch denied")

// A separate Connector keeps administrative login separate from the immutable
// pulse_user_center / new-api binding. Answer alone creates and revokes sessions.
type adminSwitchConnector struct {
	mu sync.Mutex
	db *sql.DB
}

var accountSwitch = &adminSwitchConnector{}

func init() { plugin.Register(accountSwitch) }
func (*adminSwitchConnector) Info() plugin.Info {
	text := func(s string) plugin.Translator { return plugin.Translator{Fn: func(*gin.Context) string { return s }} }
	return plugin.Info{SlugName: switchSlug, Name: text("管理员账号切换"), Description: text("允许管理员从用户管理切换到已激活的社区账号；需要 Meta Pulse 本地账号插件。"), Version: "0.1.0", Author: "nanashiwang"}
}
func (*adminSwitchConnector) ConnectorSlugName() string          { return switchSlug }
func (s *adminSwitchConnector) ConnectorName() plugin.Translator { return s.Info().Name }
func (*adminSwitchConnector) ConnectorLogoSVG() string           { return "" }
func (*adminSwitchConnector) ConnectorSender(c *gin.Context, _ string) string {
	c.Redirect(302, "/admin/users")
	return ""
}
func switchHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func switchToken(c *gin.Context) string {
	if len(c.Request.Header.Values("Authorization")) != 1 {
		return ""
	}
	return strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
}
func (s *adminSwitchConnector) database(ctx context.Context) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	g, err := NewMySQLBindingGuard(os.Getenv("FORUM_BINDING_GUARD_DSN"))
	if err != nil {
		return nil, err
	}
	db := g.(*mysqlBindingGuard).db
	if err = ensureSwitchTable(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	s.db = db
	return db, nil
}
func ensureSwitchTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS metar_admin_switch (
 ticket_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 session_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 actor_id BIGINT NOT NULL, target_id BIGINT NOT NULL,
 created_at BIGINT NOT NULL, expires_at BIGINT NOT NULL, consumed_at BIGINT NULL,
 returned_at BIGINT NULL, INDEX actor_created(actor_id,created_at)
 ) ENGINE=InnoDB`)
	return err
}

// Lock the actor and target so status changes and concurrent switch issuance
// have a definite transaction order. Missing/ambiguous roles fail closed.
func switchAccount(ctx context.Context, tx *sql.Tx, id string, admin bool) error {
	var status, mail int
	if err := tx.QueryRowContext(ctx, "SELECT status,mail_status FROM `user` WHERE id=? FOR UPDATE", id).Scan(&status, &mail); err != nil {
		return errSwitchDenied
	}
	if status != 1 || mail != 1 {
		return errSwitchDenied
	}
	rows, err := tx.QueryContext(ctx, "SELECT role_id FROM user_role_rel WHERE user_id=? FOR UPDATE", id)
	if err != nil {
		return err
	}
	defer rows.Close()
	role, count := 1, 0
	for rows.Next() {
		if err = rows.Scan(&role); err != nil {
			return err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count > 1 || admin && (count != 1 || role != 2) || !admin && role != 1 && role != 3 {
		return errSwitchDenied
	}
	return nil
}
func issueSwitch(ctx context.Context, db *sql.DB, actor, username, session string, now int64) (string, error) {
	if !canonicalCommunityID(actor) || session == "" || len(username) == 0 || len(username) > 100 {
		return "", errSwitchDenied
	}
	ticket, err := randomHex(32)
	if err != nil {
		return "", err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = switchAccount(ctx, tx, actor, true); err != nil {
		return "", err
	}
	var target string
	rows, err := tx.QueryContext(ctx, "SELECT id FROM `user` WHERE username=? LIMIT 2", username)
	if err != nil {
		return "", err
	}
	count := 0
	for rows.Next() {
		if err = rows.Scan(&target); err != nil {
			rows.Close()
			return "", err
		}
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if count != 1 || target == actor {
		return "", errSwitchDenied
	}
	if err = switchAccount(ctx, tx, target, false); err != nil {
		return "", err
	}
	var recent int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_admin_switch WHERE actor_id=? AND created_at>?", actor, now-60).Scan(&recent); err != nil {
		return "", err
	}
	if recent >= 10 {
		return "", errSwitchDenied
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO metar_admin_switch(ticket_hash,session_hash,actor_id,target_id,created_at,expires_at) VALUES(?,?,?,?,?,?)", switchHash(ticket), switchHash(session), actor, target, now, now+60)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return ticket, nil
}
func consumeSwitch(ctx context.Context, db *sql.DB, ticket, session string, now int64) (string, error) {
	if !switchProof.MatchString(ticket) || session == "" {
		return "", errSwitchDenied
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var actor, target, hash string
	var expires int64
	var consumed sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT actor_id,target_id,session_hash,expires_at,consumed_at FROM metar_admin_switch WHERE ticket_hash=? FOR UPDATE", switchHash(ticket)).Scan(&actor, &target, &hash, &expires, &consumed)
	if err != nil || consumed.Valid || expires <= now || hash != switchHash(session) {
		return "", errSwitchDenied
	}
	if err = switchAccount(ctx, tx, actor, true); err != nil {
		return "", err
	}
	if actor == target {
		return "", errSwitchDenied
	}
	if err = switchAccount(ctx, tx, target, false); err != nil {
		return "", err
	}
	// This provider is only an administrative alias for the existing local ID.
	// Never supply email: a missing/deleted mapping cannot auto-register or bind.
	external := "admin-switch:" + target
	rows, err := tx.QueryContext(ctx, "SELECT user_id,external_id FROM user_external_login WHERE provider=? AND (external_id=? OR user_id=?) FOR UPDATE", switchSlug, external, target)
	if err != nil {
		return "", err
	}
	count := 0
	for rows.Next() {
		var id, ext string
		if err = rows.Scan(&id, &ext); err != nil {
			rows.Close()
			return "", err
		}
		count++
		if id != target || ext != external {
			rows.Close()
			return "", errSwitchDenied
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if count > 1 {
		return "", errSwitchDenied
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, "INSERT INTO user_external_login(created_at,updated_at,user_id,provider,external_id,meta_info) VALUES(CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,?,?,?,'')", target, switchSlug, external)
		if err != nil {
			return "", err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE metar_admin_switch SET consumed_at=? WHERE ticket_hash=?", now, switchHash(ticket))
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return external, nil
}
func (*adminSwitchConnector) ConnectorReceiver(c *gin.Context, _ string) (plugin.ExternalLoginUserInfo, error) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	empty := plugin.ExternalLoginUserInfo{}
	// Native callback is GET. A same-origin fetch with a custom header and the
	// original admin bearer is required; no cookies or query-string credentials.
	site, e1 := url.Parse(plugin.SiteURL())
	ref, e2 := url.Parse(c.GetHeader("Referer"))
	if !plugin.StatusManager.IsEnabled(pluginSlug) || !plugin.StatusManager.IsEnabled(switchSlug) || e1 != nil || e2 != nil || site.Host == "" || ref.Scheme != site.Scheme || ref.Host != site.Host || c.GetHeader("X-Metar-Request") != "1" || c.Request.URL.RawQuery != "" {
		return empty, errSwitchDenied
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	db, err := accountSwitch.database(ctx)
	if err != nil {
		return empty, errSwitchDenied
	}
	external, err := consumeSwitch(ctx, db, c.GetHeader("X-Metar-Switch"), switchToken(c), time.Now().Unix())
	if err != nil {
		return empty, errSwitchDenied
	}
	return plugin.ExternalLoginUserInfo{ExternalID: external}, nil
}
func (uc *UserCenter) registerAccountSwitch(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	for _, operation := range []string{"start", "return"} {
		r.POST("/metar/accounts/"+operation, func(c *gin.Context) {
			c.Header("Cache-Control", "no-store")
			if !plugin.StatusManager.IsEnabled(pluginSlug) || c.Request.URL.RawQuery != "" || switchToken(c) == "" || !communitySameOrigin(c.Request, siteURL()) {
				communityError(c, 403, "forbidden")
				return
			}
			actor, err := session(c)
			if err != nil {
				communityError(c, 403, "forbidden")
				return
			}
			ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
			defer cancel()
			db, err := accountSwitch.database(ctx)
			if err != nil {
				communityError(c, 503, "switch_unavailable")
				return
			}
			if err = (&mysqlBindingGuard{db: db}).CheckAdminIdentity(ctx, actor); err != nil {
				communityError(c, 403, "forbidden")
				return
			}
			if operation == "start" {
				var body struct {
					Username string `json:"username"`
				}
				if !experienceJSON(c, &body) {
					communityError(c, 400, "invalid_request")
					return
				}
				if !plugin.StatusManager.IsEnabled(switchSlug) {
					communityError(c, 409, "switch_plugin_disabled")
					return
				}
				ticket, err := issueSwitch(ctx, db, actor, body.Username, switchToken(c), time.Now().Unix())
				if err != nil {
					communityError(c, 403, "switch_denied")
					return
				}
				c.JSON(200, gin.H{"data": gin.H{"ticket": ticket, "callback": switchCallback}})
			} else {
				var body struct {
					Ticket string `json:"ticket"`
				}
				if !experienceJSON(c, &body) || !switchProof.MatchString(body.Ticket) {
					communityError(c, 400, "invalid_request")
					return
				}
				result, err := db.ExecContext(ctx, "UPDATE metar_admin_switch SET returned_at=COALESCE(returned_at,?) WHERE ticket_hash=? AND actor_id=? AND session_hash=?", time.Now().Unix(), switchHash(body.Ticket), actor, switchHash(switchToken(c)))
				if err != nil {
					communityError(c, 503, "switch_unavailable")
					return
				}
				n, _ := result.RowsAffected()
				if n == 0 {
					var exists int
					err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metar_admin_switch WHERE ticket_hash=? AND actor_id=? AND session_hash=?", switchHash(body.Ticket), actor, switchHash(switchToken(c))).Scan(&exists)
					if err != nil || exists != 1 {
						communityError(c, 403, "forbidden")
						return
					}
				}
				c.JSON(200, gin.H{"data": gin.H{"ok": true}})
			}
		})
	}
}
