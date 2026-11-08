package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/admin"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/catalog"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/security"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

type fakeAdmin struct {
	importErr error
	actor     string
	key       string
	imports   int
}

func (f *fakeAdmin) Overview(context.Context) (mysqlstore.Overview, error) {
	return mysqlstore.Overview{}, nil
}
func (f *fakeAdmin) Export(context.Context) (catalog.Document, error) { return catalog.Document{}, nil }
func (f *fakeAdmin) Validate(context.Context, catalog.Document) (map[string]int, error) {
	return nil, &catalog.ValidationError{Problems: []string{"models[0].name bad"}}
}
func (f *fakeAdmin) Import(_ context.Context, _ catalog.Document, _ []byte, actor, key string) (admin.ImportResult, error) {
	f.imports++
	f.actor, f.key = actor, key
	return admin.ImportResult{}, f.importErr
}
func (f *fakeAdmin) Recompute(context.Context) (admin.SnapshotResult, error) {
	return admin.SnapshotResult{}, nil
}

type nonceSet map[string]bool

func (n nonceSet) ClaimNonce(_ context.Context, v string, _ time.Time) (bool, error) {
	if n[v] {
		return false, nil
	}
	n[v] = true
	return true, nil
}

var (
	adminSecret = []byte(strings.Repeat("k", 32))
	adminNow    = time.Unix(1_800_000_000, 0)
	nonceSeq    = 0
)

func adminRouter(t *testing.T, svc *fakeAdmin) http.Handler {
	t.Helper()
	r := NewRouter(nil, &fakeReader{}, fakeDB{}, time.Hour)
	if !RegisterAdmin(r, nil, svc, [][]byte{adminSecret}, nonceSet{}, func() time.Time { return adminNow }) {
		t.Fatal("admin routes not registered")
	}
	return r
}

func signedAdmin(method, path, body string) *http.Request {
	nonceSeq++
	nonce := strings.Repeat("a", 32-len(strconv.Itoa(nonceSeq))) + strconv.Itoa(nonceSeq)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	ts := adminNow.Unix()
	req.Header.Set(security.HeaderUserID, "10010000000000001")
	req.Header.Set(security.HeaderRole, "admin")
	req.Header.Set(security.HeaderTimestamp, strconv.FormatInt(ts, 10))
	req.Header.Set(security.HeaderNonce, nonce)
	req.Header.Set(security.HeaderSignature, security.Sign(adminSecret, security.CanonicalPayload(method, path, "10010000000000001", "admin", ts, nonce, []byte(body))))
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestAdminRequiresSignature(t *testing.T) {
	h := adminRouter(t, &fakeAdmin{})
	if w := serve(h, httptest.NewRequest("GET", "/v1/internal/admin/overview", nil)); w.Code != 401 {
		t.Fatalf("unsigned: %d", w.Code)
	}
	if w := serve(h, signedAdmin("GET", "/v1/internal/admin/overview", "")); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("signed: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	req := signedAdmin("GET", "/v1/internal/admin/overview", "")
	serve(h, req)
	again := httptest.NewRequest("GET", "/v1/internal/admin/overview", nil)
	again.Header = req.Header.Clone()
	if w := serve(h, again); w.Code != 401 {
		t.Fatalf("replay: %d", w.Code)
	}
}

func TestAdminNotRegisteredWithoutSecret(t *testing.T) {
	r := NewRouter(nil, &fakeReader{}, fakeDB{}, time.Hour)
	if RegisterAdmin(r, nil, &fakeAdmin{}, [][]byte{[]byte("short")}, nonceSet{}, nil) {
		t.Fatal("registered with unusable secret")
	}
	if w := serve(r, signedAdmin("GET", "/v1/internal/admin/overview", "")); w.Code != 404 {
		t.Fatalf("route exists: %d", w.Code)
	}
}

func TestAdminImportContract(t *testing.T) {
	svc := &fakeAdmin{}
	h := adminRouter(t, svc)
	body := `{"source":{"slug":"x"}}`
	if w := serve(h, signedAdmin("POST", "/v1/internal/admin/import", body)); w.Code != 400 {
		t.Fatalf("missing key: %d", w.Code)
	}
	req := signedAdmin("POST", "/v1/internal/admin/import", body)
	req.Header.Set("Idempotency-Key", "0f8fad5b-d9cb-469f-a165-70867728950e")
	if w := serve(h, req); w.Code != 200 || svc.actor != "10010000000000001" || svc.key != "0f8fad5b-d9cb-469f-a165-70867728950e" {
		t.Fatalf("import: %d actor=%q key=%q", w.Code, svc.actor, svc.key)
	}
	bad := signedAdmin("POST", "/v1/internal/admin/import", `{"source":{"slug":"x"},"typo":1}`)
	bad.Header.Set("Idempotency-Key", "1f8fad5b-d9cb-469f-a165-70867728950e")
	if w := serve(h, bad); w.Code != 422 || svc.imports != 1 {
		t.Fatalf("unknown field: %d imports=%d", w.Code, svc.imports)
	}
	svc.importErr = mysqlstore.ErrIdempotencyConflict
	conflict := signedAdmin("POST", "/v1/internal/admin/import", body)
	conflict.Header.Set("Idempotency-Key", "0f8fad5b-d9cb-469f-a165-70867728950e")
	if w := serve(h, conflict); w.Code != 409 {
		t.Fatalf("conflict: %d", w.Code)
	}
	svc.importErr = errors.New("db down")
	down := signedAdmin("POST", "/v1/internal/admin/import", body)
	down.Header.Set("Idempotency-Key", "2f8fad5b-d9cb-469f-a165-70867728950e")
	if w := serve(h, down); w.Code != 503 || strings.Contains(w.Body.String(), "db down") {
		t.Fatalf("outage must not leak detail: %d %s", w.Code, w.Body.String())
	}
}

func TestAdminValidateReturnsProblems(t *testing.T) {
	h := adminRouter(t, &fakeAdmin{})
	w := serve(h, signedAdmin("POST", "/v1/internal/admin/validate", `{"source":{"slug":"x"}}`))
	if w.Code != 422 || !strings.Contains(w.Body.String(), "models[0].name bad") {
		t.Fatalf("validate: %d %s", w.Code, w.Body.String())
	}
	if w := serve(h, signedAdmin("POST", "/v1/internal/admin/recompute", "{}")); w.Code != 400 {
		t.Fatalf("recompute with body: %d", w.Code)
	}
}
