package pulse_user_center

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
)

var operatorPublisher struct {
	sync.RWMutex
	call func(*gin.Context, string, string, []byte) (int, []byte)
}

func (uc *UserCenter) RegisterOperatorPublisher(call func(*gin.Context, string, string, []byte) (int, []byte)) {
	operatorPublisher.Lock()
	defer operatorPublisher.Unlock()
	operatorPublisher.call = call
}

var publishingEnabled = func() bool { return plugin.StatusManager.IsEnabled(pluginSlug) }
var publishingDatabase = func(ctx context.Context) (*sql.DB, error) { return accountSwitch.database(ctx) }

var publishKey = regexp.MustCompile("^[a-zA-Z0-9-]{16,64}$")

type publishingItem struct {
	Key            string   `json:"key"`
	Username       string   `json:"username"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	Tags           []string `json:"tags"`
	QuestionID     string   `json:"question_id"`
	Source         string   `json:"source"`
	Reason         string   `json:"reason"`
	ObjectID       string   `json:"object_id,omitempty"`
	ReplyCommentID string   `json:"reply_comment_id,omitempty"`
}

func validatePublishingItem(p publishingItem) error {
	if !publishKey.MatchString(p.Key) || len(p.Username) == 0 || len(p.Username) > 100 || utf8.RuneCountInString(strings.TrimSpace(p.Content)) < 6 || len(p.Content) > 50000 || utf8.RuneCountInString(strings.TrimSpace(p.Reason)) < 2 || len(p.Reason) > 600 {
		return errors.New("invalid_item")
	}
	if p.Kind == "question" {
		if utf8.RuneCountInString(p.Title) < 6 || utf8.RuneCountInString(p.Title) > 150 || len(p.Tags) < 1 || len(p.Tags) > 5 {
			return errors.New("invalid_question")
		}
		for _, tag := range p.Tags {
			if strings.TrimSpace(tag) == "" || len(tag) > 100 {
				return errors.New("invalid_tag")
			}
		}
	} else if p.Kind == "answer" {
		if !canonicalCommunityID(p.QuestionID) {
			return errors.New("invalid_question_id")
		}
	} else if p.Kind == "comment" {
		if !canonicalCommunityID(p.ObjectID) || (p.ReplyCommentID != "" && !canonicalCommunityID(p.ReplyCommentID)) || utf8.RuneCountInString(p.Content) > 400 || p.Source != "" {
			return errors.New("invalid_comment")
		}
	} else {
		return errors.New("invalid_kind")
	}
	if p.Source != "" {
		u, e := url.Parse(p.Source)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(p.Source) > 1500 || strings.ContainsAny(p.Source, "\r\n<>()[]") {
			return errors.New("invalid_source")
		}
	}
	return nil
}
func publishingPayload(p publishingItem, marker string) []byte {
	content := p.Content + "\n\n---\n**运营代发 / Posted by community operations** · " + marker
	if p.Source != "" {
		content += "\n\n来源 / Source: " + p.Source
	}
	if p.Kind == "comment" {
		raw, _ := json.Marshal(map[string]any{"original_text": content, "object_id": p.ObjectID, "reply_comment_id": p.ReplyCommentID})
		return raw
	}
	payload := map[string]any{"content": content}
	if p.Kind == "answer" {
		payload["question_id"] = p.QuestionID
	} else {
		payload["title"] = p.Title
		tags := make([]map[string]string, 0, len(p.Tags))
		for _, tag := range p.Tags {
			tags = append(tags, map[string]string{"slug_name": tag, "display_name": tag})
		}
		payload["tags"] = tags
	}
	raw, _ := json.Marshal(payload)
	return raw
}
func ensurePublishing(ctx context.Context, db *sql.DB) error {
	for _, query := range []string{
		"CREATE TABLE IF NOT EXISTS metar_operator_account(user_id BIGINT UNSIGNED PRIMARY KEY,actor_id BIGINT UNSIGNED NOT NULL,reason VARCHAR(600) NOT NULL,created_at DATETIME(6) NOT NULL)",
		"CREATE TABLE IF NOT EXISTS metar_operator_publication(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,actor_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,target_id BIGINT UNSIGNED NOT NULL,username VARCHAR(100) NOT NULL,kind VARCHAR(16) NOT NULL,payload_hash CHAR(64) NOT NULL,payload MEDIUMTEXT NOT NULL,marker VARCHAR(100) NOT NULL,status VARCHAR(24) NOT NULL,object_id VARCHAR(32) NOT NULL DEFAULT '',question_id VARCHAR(32) NOT NULL DEFAULT '',message TEXT NOT NULL,created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,UNIQUE request_identity(actor_id,request_key),UNIQUE marker_identity(marker))",
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

// Only explicitly enrolled, unbound, active ordinary accounts may be used.
func operatorTarget(ctx context.Context, db *sql.DB, username string, enrolled bool) (string, error) {
	var id string
	query := "SELECT u.id FROM user u JOIN user_role_rel r ON r.user_id=u.id WHERE u.username=? AND u.status=1 AND u.mail_status=1 AND r.role_id=1 AND (SELECT COUNT(*) FROM user_role_rel rr WHERE rr.user_id=u.id)=1 AND NOT EXISTS(SELECT 1 FROM user_external_login e WHERE e.user_id=u.id AND e.provider<>'metar_admin_switch')"
	if enrolled {
		query += " AND EXISTS(SELECT 1 FROM metar_operator_account o WHERE o.user_id=u.id)"
	}
	rows, err := db.QueryContext(ctx, query, username)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", errors.New("operator_account_required")
	}
	if err = rows.Scan(&id); err != nil {
		return "", err
	}
	if rows.Next() {
		return "", errors.New("ambiguous_account")
	}
	return id, rows.Err()
}
func (uc *UserCenter) registerPublishing(r *gin.RouterGroup, session func(*gin.Context) (string, error), siteURL func() string) {
	authorize := func(c *gin.Context) (string, *sql.DB, bool) {
		c.Header("Cache-Control", "no-store")
		internalActor, internal := c.Get(publishingInternalActor)
		if !publishingEnabled() || c.Request.URL.RawQuery != "" || (!internal && switchToken(c) == "") {
			communityError(c, 403, "forbidden")
			return "", nil, false
		}
		if !internal && c.Request.Method != "GET" && (c.GetHeader("X-Metar-Request") != "1" || !communitySameOrigin(c.Request, siteURL())) {
			communityError(c, 403, "forbidden")
			return "", nil, false
		}
		var actor string
		var err error
		if internal {
			actor = internalActor.(string)
		} else {
			actor, err = session(c)
		}
		if err != nil {
			adminSettingsIdentityError(c, err)
			return "", nil, false
		}
		uc.runtimeMu.RLock()
		guard, ok := uc.Guard.(adminIdentityReader)
		if !ok {
			uc.runtimeMu.RUnlock()
			communityError(c, 503, "unavailable")
			return "", nil, false
		}
		err = guard.CheckAdminIdentity(c.Request.Context(), actor)
		uc.runtimeMu.RUnlock()
		if err != nil {
			adminSettingsIdentityError(c, err)
			return "", nil, false
		}
		db, err := publishingDatabase(c.Request.Context())
		if err != nil {
			communityError(c, 503, "unavailable")
			return "", nil, false
		}
		if err = ensurePublishing(c.Request.Context(), db); err != nil {
			communityError(c, 503, "unavailable")
			return "", nil, false
		}
		return actor, db, true
	}
	r.GET("/metar/publishing", func(c *gin.Context) {
		_, db, ok := authorize(c)
		if !ok {
			return
		}
		ctx := c.Request.Context()
		rows, err := db.QueryContext(ctx, "SELECT u.username,u.display_name FROM metar_operator_account o JOIN user u ON u.id=o.user_id ORDER BY o.created_at DESC LIMIT 200")
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		accounts := []map[string]string{}
		for rows.Next() {
			var name, display string
			if err = rows.Scan(&name, &display); err != nil {
				break
			}
			accounts = append(accounts, map[string]string{"username": name, "display_name": display})
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		history, err := db.QueryContext(ctx, "SELECT actor_id,username,kind,status,object_id,question_id,message,request_key FROM metar_operator_publication ORDER BY id DESC LIMIT 100")
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		defer history.Close()
		items := []map[string]string{}
		for history.Next() {
			var actor, name, kind, status, object, question, message, key string
			if err = history.Scan(&actor, &name, &kind, &status, &object, &question, &message, &key); err != nil {
				communityError(c, 503, "unavailable")
				return
			}
			items = append(items, map[string]string{"actor": actor, "username": name, "kind": kind, "status": status, "object_id": object, "question_id": question, "message": message, "key": key})
		}
		if history.Err() != nil {
			communityError(c, 503, "unavailable")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"accounts": accounts, "history": items}})
	})
	r.POST("/metar/publishing/accounts", func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		var body struct {
			Username string `json:"username"`
			Reason   string `json:"reason"`
		}
		if !publishingJSON(c, &body) || len(body.Username) > 100 || len(body.Reason) > 600 || utf8.RuneCountInString(strings.TrimSpace(body.Reason)) < 2 {
			communityError(c, 400, "invalid_request")
			return
		}
		id, err := operatorTarget(c.Request.Context(), db, body.Username, false)
		if err != nil {
			communityError(c, 400, "requires_active_unbound_ordinary_account")
			return
		}
		_, err = db.ExecContext(c.Request.Context(), "INSERT IGNORE INTO metar_operator_account(user_id,actor_id,reason,created_at) VALUES(?,?,?,NOW(6))", id, actor, body.Reason)
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"username": body.Username}})
	})
	itemHandler := func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		var item publishingItem
		if !publishingJSON(c, &item) || validatePublishingItem(item) != nil {
			communityError(c, 400, "invalid_item")
			return
		}
		operatorPublisher.RLock()
		publish := operatorPublisher.call
		operatorPublisher.RUnlock()
		if publish == nil {
			communityError(c, 503, "publishing_adapter_unavailable")
			return
		}
		target, err := operatorTarget(c.Request.Context(), db, item.Username, true)
		if err != nil {
			communityError(c, 403, "operator_account_required")
			return
		}
		raw, _ := json.Marshal(item)
		digest := sha256.Sum256(raw)
		hash := hex.EncodeToString(digest[:])
		marker := "metar-ops-" + actor + "-" + item.Key
		// Serialize per-item dispatch in MySQL across all instances. If a response is
		// lost, the durable running record prevents a second native create attempt.
		conn, err := db.Conn(c.Request.Context())
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		defer conn.Close()
		lockHash := sha256.Sum256([]byte(marker))
		lockName := "ops:" + hex.EncodeToString(lockHash[:])[:56]
		actorLock := "ops-actor:" + actor
		var locked int
		if err = conn.QueryRowContext(c.Request.Context(), "SELECT GET_LOCK(?,0)", actorLock).Scan(&locked); err != nil || locked != 1 {
			communityError(c, 409, "publication_busy")
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", actorLock)
		}()
		if err = conn.QueryRowContext(c.Request.Context(), "SELECT GET_LOCK(?,0)", lockName).Scan(&locked); err != nil || locked != 1 {
			communityError(c, 409, "publication_busy")
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", lockName)
		}()
		var previousHash, status, object, question, message string
		err = conn.QueryRowContext(c.Request.Context(), "SELECT payload_hash,status,object_id,question_id,message FROM metar_operator_publication WHERE actor_id=? AND request_key=?", actor, item.Key).Scan(&previousHash, &status, &object, &question, &message)
		if err == nil {
			if previousHash != hash {
				communityError(c, 409, "idempotency_conflict")
				return
			}
			// Unknown native results are never automatically reissued. Reconcile by the
			// unique visible marker to recover writes whose HTTP result was lost.
			if status == "running" || status == "needs_review" {
				table := "question"
				if item.Kind == "answer" || item.Kind == "comment" {
					table = item.Kind
				}
				var recovered string
				e := conn.QueryRowContext(c.Request.Context(), "SELECT id FROM "+table+" WHERE user_id=? AND LOCATE(?,original_text)>0 ORDER BY id LIMIT 1", target, marker).Scan(&recovered)
				if e == nil {
					status = "published"
					object = recovered
					message = "发布记录已核对"
					_, _ = conn.ExecContext(c.Request.Context(), "UPDATE metar_operator_publication SET status=?,object_id=?,message=?,updated_at=NOW(6) WHERE actor_id=? AND request_key=?", status, object, message, actor, item.Key)
				} else {
					status = "needs_review"
					message = "结果待核对，请勿重新创建同一内容"
				}
			}
			c.JSON(200, gin.H{"data": gin.H{"status": status, "object_id": object, "question_id": question, "message": message}})
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			communityError(c, 503, "unavailable")
			return
		}
		var recent int
		if err = conn.QueryRowContext(c.Request.Context(), "SELECT COUNT(*) FROM metar_operator_publication WHERE actor_id=? AND created_at>DATE_SUB(NOW(),INTERVAL 1 MINUTE)", actor).Scan(&recent); err != nil || recent >= 20 {
			communityError(c, 429, "publishing_rate_limit")
			return
		}
		_, err = conn.ExecContext(c.Request.Context(), "INSERT INTO metar_operator_publication(actor_id,request_key,target_id,username,kind,payload_hash,payload,marker,status,question_id,message,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'running',?,'发布中',NOW(6),NOW(6))", actor, item.Key, target, item.Username, item.Kind, hash, string(raw), marker, item.QuestionID)
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		code, result := publish(c, target, item.Kind, publishingPayload(item, marker))
		var response struct {
			Data struct {
				ID        string `json:"id"`
				CommentID string `json:"comment_id"`
				Info      struct {
					ID string `json:"id"`
				} `json:"info"`
			} `json:"data"`
			Message string `json:"msg"`
		}
		_ = json.Unmarshal(result, &response)
		object = response.Data.ID
		if item.Kind == "comment" {
			object = response.Data.CommentID
		}
		if object == "" {
			object = response.Data.Info.ID
		}
		status = "needs_review"
		message = "发布结果待核对；请检查内容和原生权限，不要直接重复创建"
		if code == http.StatusOK && canonicalCommunityID(object) {
			status = "published"
			message = "已提交，是否公开遵循社区审核规则"
		} else if response.Message != "" {
			message = fmt.Sprintf("原生接口返回 %d：%s；请核对后处理", code, response.Message)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err = conn.ExecContext(ctx, "UPDATE metar_operator_publication SET status=?,object_id=?,message=?,updated_at=NOW(6) WHERE actor_id=? AND request_key=?", status, object, message, actor, item.Key)
		if err != nil {
			communityError(c, 503, "result_unknown")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"status": status, "object_id": object, "question_id": item.QuestionID, "message": message}})
	}
	r.POST("/metar/publishing/item", itemHandler)
	uc.registerPublishingPlans(r, authorize, itemHandler)
}

func publishingJSON(c *gin.Context, value any) bool {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 65537))
	if err != nil || len(raw) > 65536 || !uniqueExperienceJSON(raw) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil
}
