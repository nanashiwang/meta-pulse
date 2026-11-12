package pulse_user_center

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func validPublishingItem() publishingItem {
	return publishingItem{Key: "12345678-1234-1234", Username: "editor", Kind: "question", Title: "一个完整的话题标题", Content: "需要讨论的实际内容", Tags: []string{"news"}, Reason: "社区运营"}
}
func TestPublishingValidationAndDisclosure(t *testing.T) {
	good := validPublishingItem()
	if err := validatePublishingItem(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*publishingItem){func(p *publishingItem) { p.Kind = "comment" }, func(p *publishingItem) { p.Content = "" }, func(p *publishingItem) { p.Key = "bad" }, func(p *publishingItem) { p.Source = "javascript:alert(1)" }, func(p *publishingItem) { p.Source = "https://a.test/\nadmin" }, func(p *publishingItem) { p.Kind = "answer"; p.QuestionID = "../../admin" }} {
		p := good
		change(&p)
		if validatePublishingItem(p) == nil {
			t.Fatalf("accepted invalid %+v", p)
		}
	}
	var body map[string]any
	if err := json.Unmarshal(publishingPayload(good, "metar-ops-7-key"), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["content"].(string), "运营代发") || body["user_id"] != nil || body["html"] != nil {
		t.Fatal(body)
	}
}

type publicationRecord struct{ hash, status, object, question, message string }
type publishingDriver struct {
	record    *publicationRecord
	calls     int
	recoverID string
}
type publishingConnector struct{ state *publishingDriver }

func (c publishingConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c publishingConnector) Driver() driver.Driver                        { return c.state }
func (d *publishingDriver) Open(string) (driver.Conn, error)               { return d, nil }
func (d *publishingDriver) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *publishingDriver) Close() error { return nil }
func (d *publishingDriver) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}

type publishingRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *publishingRows) Columns() []string { return r.columns }
func (r *publishingRows) Close() error      { return nil }
func (r *publishingRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func one(values ...driver.Value) driver.Rows {
	columns := make([]string, len(values))
	for i := range columns {
		columns[i] = "value"
	}
	return &publishingRows{columns: columns, values: [][]driver.Value{values}}
}
func (d *publishingDriver) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "GET_LOCK"):
		return one(int64(1)), nil
	case strings.Contains(q, "SELECT u.id FROM user"):
		if args[0].Value != "editor" {
			return &publishingRows{columns: []string{"id"}}, nil
		}
		return one("23"), nil
	case strings.Contains(q, "SELECT payload_hash"):
		if d.record == nil {
			return &publishingRows{columns: []string{"hash", "status", "object", "question", "message"}}, nil
		}
		r := d.record
		return one(r.hash, r.status, r.object, r.question, r.message), nil
	case strings.Contains(q, "SELECT COUNT(*) FROM metar_operator_publication"):
		return one(int64(0)), nil
	case strings.Contains(q, "LOCATE"):
		if d.recoverID == "" {
			return &publishingRows{columns: []string{"id"}}, nil
		}
		return one(d.recoverID), nil
	}
	return nil, errors.New("unexpected query " + q)
}
func (d *publishingDriver) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "CREATE TABLE") || strings.Contains(q, "RELEASE_LOCK") {
		return driver.RowsAffected(0), nil
	}
	if strings.HasPrefix(q, "INSERT INTO metar_operator_publication") {
		d.record = &publicationRecord{hash: args[5].Value.(string), status: "running", question: args[8].Value.(string)}
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(q, "UPDATE metar_operator_publication") {
		d.record.status = args[0].Value.(string)
		d.record.object = args[1].Value.(string)
		d.record.message = args[2].Value.(string)
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected exec " + q)
}
func publishingFixture(t *testing.T, code int) (*gin.Engine, *publishingDriver, *adminTestGuard) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	state := &publishingDriver{}
	db := sql.OpenDB(publishingConnector{state})
	db.SetMaxOpenConns(1)
	oldDB, oldEnabled := publishingDatabase, publishingEnabled
	publishingDatabase = func(context.Context) (*sql.DB, error) { return db, nil }
	publishingEnabled = func() bool { return true }
	guard := &adminTestGuard{}
	uc := &UserCenter{Guard: guard}
	operatorPublisher.RLock()
	oldPublish := operatorPublisher.call
	operatorPublisher.RUnlock()
	uc.RegisterOperatorPublisher(func(_ *gin.Context, target, kind string, body []byte) (int, []byte) {
		state.calls++
		if target != "23" || !strings.Contains(string(body), "运营代发") {
			t.Error("bad attribution", target, string(body))
		}
		return code, []byte(`{"data":{"id":"123"},"msg":"result"}`)
	})
	t.Cleanup(func() {
		publishingDatabase = oldDB
		publishingEnabled = oldEnabled
		uc.RegisterOperatorPublisher(oldPublish)
		db.Close()
	})
	r := gin.New()
	uc.registerPublishing(r.Group("/answer/admin/api"), func(*gin.Context) (string, error) { return "7", nil }, func() string { return "https://metar.example.test" })
	return r, state, guard
}
func publishingRequest(router *gin.Engine, item publishingItem, origin string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(item)
	req := communityTestRequest("POST", "x", string(raw))
	req.URL.Path = "/answer/admin/api/metar/publishing/item"
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}
func TestPublishingIdempotency(t *testing.T) {
	r, state, _ := publishingFixture(t, 200)
	item := validPublishingItem()
	for i := 0; i < 3; i++ {
		w := publishingRequest(r, item, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "published") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if state.calls != 1 {
		t.Fatal("duplicate native publication", state.calls)
	}
	item.Title = "不同内容不能复用请求编号"
	w := publishingRequest(r, item, "")
	if w.Code != 409 || state.calls != 1 {
		t.Fatal(w.Code, state.calls)
	}
}
func TestPublishingUnknownNeverReissues(t *testing.T) {
	r, state, _ := publishingFixture(t, 500)
	item := validPublishingItem()
	publishingRequest(r, item, "")
	w := publishingRequest(r, item, "")
	if state.calls != 1 || !strings.Contains(w.Body.String(), "needs_review") {
		t.Fatal(state.calls, w.Body.String())
	}
	state.recoverID = "456"
	w = publishingRequest(r, item, "")
	if state.calls != 1 || !strings.Contains(w.Body.String(), "published") {
		t.Fatal(state.calls, w.Body.String())
	}
}
func TestPublishingPermissions(t *testing.T) {
	r, state, guard := publishingFixture(t, 200)
	item := validPublishingItem()
	if w := publishingRequest(r, item, "https://evil.test"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	guard.err = errAdminForbidden
	if w := publishingRequest(r, item, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	guard.err = nil
	item.Username = "real-person"
	if w := publishingRequest(r, item, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if state.calls != 0 {
		t.Fatal("unauthorized publication")
	}
}
