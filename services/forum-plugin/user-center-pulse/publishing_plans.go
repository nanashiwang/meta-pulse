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
	"net/http/httptest"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

// This key is only populated by the in-process runner, never from a request.
const publishingInternalActor = "metar.internal.publishing.actor"

var planID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)

type publishingPlanItem struct {
	publishingItem
	ID        string `json:"id"`
	Parent    string `json:"parent,omitempty"`
	PublishAt string `json:"publish_at,omitempty"`
}
type publishingPlan struct {
	Key   string               `json:"key"`
	Items []publishingPlanItem `json:"items"`
}
type publishingPlanResult struct {
	Status     string `json:"status"`
	ObjectID   string `json:"object_id"`
	QuestionID string `json:"question_id"`
	Message    string `json:"message"`
}

func validatePublishingPlan(p publishingPlan) error {
	if !publishKey.MatchString(p.Key) || len(p.Items) == 0 || len(p.Items) > 20 {
		return errors.New("invalid_plan")
	}
	seen := map[string]publishingPlanItem{}
	for _, entry := range p.Items {
		if !planID.MatchString(entry.ID) {
			return errors.New("invalid_id")
		}
		if _, ok := seen[entry.ID]; ok {
			return errors.New("duplicate_id")
		}
		if entry.PublishAt != "" {
			if _, err := time.Parse(time.RFC3339, entry.PublishAt); err != nil {
				return errors.New("invalid_time_requires_timezone")
			}
		}
		item := entry.publishingItem
		item.Key = "12345678-12345678"
		if entry.Parent != "" {
			parent, ok := seen[entry.Parent]
			if !ok || item.Kind == "question" || item.QuestionID != "" || item.ObjectID != "" || item.ReplyCommentID != "" {
				return errors.New("invalid_parent")
			}
			if item.Kind == "answer" {
				if parent.Kind != "question" {
					return errors.New("answer_requires_question")
				}
				item.QuestionID = "1"
			} else {
				item.ObjectID = "1"
			}
		}
		if err := validatePublishingItem(item); err != nil {
			return err
		}
		seen[entry.ID] = entry
	}
	return nil
}
func resolvePlanItem(plan publishingPlan, entry publishingPlanItem, results map[string]publishingPlanResult) (publishingItem, bool) {
	item := entry.publishingItem
	sum := sha256.Sum256([]byte(plan.Key + ":" + entry.ID))
	item.Key = hex.EncodeToString(sum[:])
	if entry.Parent == "" {
		return item, true
	}
	result, ok := results[entry.Parent]
	if !ok || result.Status != "published" {
		return item, false
	}
	var parent publishingPlanItem
	for _, p := range plan.Items {
		if p.ID == entry.Parent {
			parent = p
			break
		}
	}
	if item.Kind == "answer" {
		item.QuestionID = result.ObjectID
		return item, true
	}
	if parent.Kind == "comment" {
		resolved, ok := resolvePlanItem(plan, parent, results)
		if !ok {
			return item, false
		}
		item.ObjectID = resolved.ObjectID
		item.ReplyCommentID = result.ObjectID
		item.QuestionID = resolved.QuestionID
	} else {
		item.ObjectID = result.ObjectID
		item.QuestionID = result.QuestionID
		if parent.Kind == "question" {
			item.QuestionID = result.ObjectID
		}
	}
	return item, true
}
func ensurePublishingPlans(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS metar_operator_plan(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,actor_id BIGINT UNSIGNED NOT NULL,request_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,payload_hash CHAR(64) NOT NULL,payload MEDIUMTEXT NOT NULL,results MEDIUMTEXT NOT NULL,status VARCHAR(24) NOT NULL,created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,UNIQUE plan_identity(actor_id,request_key),INDEX pending_plan(status,id))`)
	return err
}

type publishingAuthorize func(*gin.Context) (string, *sql.DB, bool)

func (uc *UserCenter) registerPublishingPlans(r *gin.RouterGroup, authorize publishingAuthorize, dispatch gin.HandlerFunc) {
	r.POST("/metar/publishing/plans", func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		var plan publishingPlan
		if !publishingJSON(c, &plan) || validatePublishingPlan(plan) != nil {
			communityError(c, 400, "invalid_plan")
			return
		}
		for i, item := range plan.Items {
			plan.Items[i].Key = ""
			if _, err := operatorTarget(c.Request.Context(), db, item.Username, true); err != nil {
				communityError(c, 403, "operator_account_required")
				return
			}
		}
		if ensurePublishingPlans(c.Request.Context(), db) != nil {
			communityError(c, 503, "unavailable")
			return
		}
		raw, _ := json.Marshal(plan)
		sum := sha256.Sum256(raw)
		hash := hex.EncodeToString(sum[:])
		// INSERT IGNORE plus a unique key makes a lost submission response safe to retry.
		_, err := db.ExecContext(c.Request.Context(), `INSERT IGNORE INTO metar_operator_plan(actor_id,request_key,payload_hash,payload,results,status,created_at,updated_at) VALUES(?,?,?,?,'{}','queued',NOW(6),NOW(6))`, actor, plan.Key, hash, string(raw))
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		var old, status string
		if db.QueryRowContext(c.Request.Context(), "SELECT payload_hash,status FROM metar_operator_plan WHERE actor_id=? AND request_key=?", actor, plan.Key).Scan(&old, &status) != nil {
			communityError(c, 503, "unavailable")
			return
		}
		if old != hash {
			communityError(c, 409, "idempotency_conflict")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"status": status, "key": plan.Key}})
	})
	r.GET("/metar/publishing/plans", func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		if ensurePublishingPlans(c.Request.Context(), db) != nil {
			communityError(c, 503, "unavailable")
			return
		}
		rows, err := db.QueryContext(c.Request.Context(), "SELECT request_key,payload,results,status FROM metar_operator_plan WHERE actor_id=? ORDER BY id DESC LIMIT 100", actor)
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		defer rows.Close()
		data := []map[string]any{}
		for rows.Next() {
			var key, payload, results, status string
			if rows.Scan(&key, &payload, &results, &status) != nil {
				communityError(c, 503, "unavailable")
				return
			}
			data = append(data, map[string]any{"key": key, "plan": json.RawMessage(payload), "results": json.RawMessage(results), "status": status})
		}
		if rows.Err() != nil {
			communityError(c, 503, "unavailable")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"plans": data}})
	})
	r.POST("/metar/publishing/plans/cancel", func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		var req struct {
			Key string `json:"key"`
		}
		if !publishingJSON(c, &req) || !publishKey.MatchString(req.Key) {
			communityError(c, 400, "invalid_request")
			return
		}
		conn, err := db.Conn(c.Request.Context())
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		defer conn.Close()
		unlock, err := lockPublishingPlan(c.Request.Context(), conn, actor, req.Key)
		if err != nil {
			communityError(c, 409, "plan_busy_retry")
			return
		}
		defer unlock()
		result, err := conn.ExecContext(c.Request.Context(), "UPDATE metar_operator_plan SET status='cancelled',updated_at=NOW(6) WHERE actor_id=? AND request_key=? AND status IN ('queued','needs_review')", actor, req.Key)
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		n, _ := result.RowsAffected()
		if n != 1 {
			communityError(c, 409, "plan_not_cancellable")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"status": "cancelled"}})
	})
	r.POST("/metar/publishing/plans/retry", func(c *gin.Context) {
		actor, db, ok := authorize(c)
		if !ok {
			return
		}
		var req struct {
			Key string `json:"key"`
		}
		if !publishingJSON(c, &req) || !publishKey.MatchString(req.Key) {
			communityError(c, 400, "invalid_request")
			return
		}
		conn, err := db.Conn(c.Request.Context())
		if err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		defer conn.Close()
		unlock, err := lockPublishingPlan(c.Request.Context(), conn, actor, req.Key)
		if err != nil {
			communityError(c, 409, "plan_busy_retry")
			return
		}
		defer unlock()
		var raw, status string
		if conn.QueryRowContext(c.Request.Context(), "SELECT results,status FROM metar_operator_plan WHERE actor_id=? AND request_key=?", actor, req.Key).Scan(&raw, &status) != nil || status != "needs_review" {
			communityError(c, 409, "plan_not_paused")
			return
		}
		results := map[string]publishingPlanResult{}
		if json.Unmarshal([]byte(raw), &results) != nil {
			communityError(c, 503, "invalid_progress")
			return
		}
		for id, result := range results {
			if result.Status != "published" {
				delete(results, id)
			}
		}
		encoded, _ := json.Marshal(results)
		if _, err = conn.ExecContext(c.Request.Context(), "UPDATE metar_operator_plan SET results=?,status='queued',updated_at=NOW(6) WHERE actor_id=? AND request_key=?", string(encoded), actor, req.Key); err != nil {
			communityError(c, 503, "unavailable")
			return
		}
		c.JSON(200, gin.H{"data": gin.H{"status": "queued"}})
	})

	publishingStartRunner(func() { uc.runPublishingPlans(dispatch) })
}

var publishingStartRunner = func(run func()) {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Printf("publishing runner interrupted: %T\n", r)
					}
				}()
				run()
			}()
		}
	}()
}

func lockPublishingPlan(ctx context.Context, conn *sql.Conn, actor, key string) (func(), error) {
	sum := sha256.Sum256([]byte(actor + ":" + key))
	name := "plan:" + hex.EncodeToString(sum[:])[:56]
	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&locked); err != nil || locked != 1 {
		return nil, errors.New("plan_busy")
	}
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", name)
	}, nil
}
func (uc *UserCenter) runPublishingPlans(dispatch gin.HandlerFunc) {
	if !publishingEnabled() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := publishingDatabase(ctx)
	if err != nil || ensurePublishingPlans(ctx, db) != nil {
		return
	}
	rows, err := db.QueryContext(ctx, "SELECT actor_id,request_key FROM metar_operator_plan WHERE status='queued' ORDER BY updated_at,id LIMIT 20")
	if err != nil {
		return
	}
	type identity struct{ actor, key string }
	var ids []identity
	for rows.Next() {
		var id identity
		if rows.Scan(&id.actor, &id.key) != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		uc.runPublishingPlan(ctx, db, id.actor, id.key, dispatch)
	}
}
func (uc *UserCenter) runPublishingPlan(ctx context.Context, db *sql.DB, actor, key string, dispatch gin.HandlerFunc) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	unlock, err := lockPublishingPlan(ctx, conn, actor, key)
	if err != nil {
		return
	}
	defer unlock()
	var raw, progress, status string
	if conn.QueryRowContext(ctx, "SELECT payload,results,status FROM metar_operator_plan WHERE actor_id=? AND request_key=?", actor, key).Scan(&raw, &progress, &status) != nil || status != "queued" {
		return
	}
	var plan publishingPlan
	results := map[string]publishingPlanResult{}
	if json.Unmarshal([]byte(raw), &plan) != nil || json.Unmarshal([]byte(progress), &results) != nil || validatePublishingPlan(plan) != nil {
		return
	}
	for _, entry := range plan.Items {
		if _, done := results[entry.ID]; done {
			continue
		}
		if entry.PublishAt != "" {
			due, _ := time.Parse(time.RFC3339, entry.PublishAt)
			if time.Now().Before(due) {
				continue
			}
		}
		item, ready := resolvePlanItem(plan, entry, results)
		if !ready {
			continue
		}
		body, _ := json.Marshal(item)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/answer/admin/api/metar/publishing/item", bytes.NewReader(body)).WithContext(ctx)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(publishingInternalActor, actor)
		dispatch(c)
		var response struct {
			Data publishingPlanResult `json:"data"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Data.Status == "" {
			response.Data = publishingPlanResult{Status: "needs_review", Message: fmt.Sprintf("请求未确认（HTTP %d），请核对单项审计", rec.Code)}
		}
		results[entry.ID] = response.Data
		if response.Data.Status != "published" {
			status = "needs_review"
		} else if len(results) == len(plan.Items) {
			status = "completed"
		}
		encoded, _ := json.Marshal(results)
		if _, err = conn.ExecContext(ctx, "UPDATE metar_operator_plan SET results=?,status=?,updated_at=NOW(6) WHERE actor_id=? AND request_key=?", string(encoded), status, actor, key); err != nil {
			return
		}
		if status != "queued" {
			return
		}
	}
	// Rotate waiting plans so distant schedules cannot starve newly due work.
	_, _ = conn.ExecContext(ctx, "UPDATE metar_operator_plan SET updated_at=NOW(6) WHERE actor_id=? AND request_key=?", actor, key)
}
