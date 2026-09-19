package runtimeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/config"
)

type memoryStore struct {
	mu       sync.Mutex
	record   Record
	receipts map[string]struct {
		hash string
		view View
	}
	writes  int
	failure error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{record: Record{Roles: map[string]Registration{}, Secrets: map[string]Secret{}}, receipts: map[string]struct {
		hash string
		view View
	}{}}
}
func (s *memoryStore) Register(_ context.Context, value Registration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.record.Roles[value.Role]; ok {
		if !reflect.DeepEqual(old.EnvironmentFingerprints, value.EnvironmentFingerprints) {
			return ErrConflict
		}
		if !bytes.Equal(old.PublicKey, value.PublicKey) {
			return ErrKeyMismatch
		}
		return nil
	}
	s.record.Roles[value.Role] = value
	if err := ValidateRecord(s.record); err != nil {
		delete(s.record.Roles, value.Role)
		return err
	}
	s.record.Revision++
	return nil
}
func (s *memoryStore) Read(_ context.Context, role string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return Record{}, s.failure
	}
	r := cloneRecord(s.record)
	for name, secret := range r.Secrets {
		if secret.Role != role {
			secret.Ciphertext = nil
			r.Secrets[name] = secret
		}
	}
	return r, nil
}
func (s *memoryStore) Update(_ context.Context, w WriteRequest, apply func(Record) (Mutation, error)) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return View{}, s.failure
	}
	key := w.ActorID + ":" + w.RequestID
	if receipt, exists := s.receipts[key]; exists {
		if receipt.hash != w.PayloadHash {
			return View{}, ErrConflict
		}
		return receipt.view, nil
	}
	if s.record.Revision != w.Revision {
		return View{}, ErrConflict
	}
	change, err := apply(cloneRecord(s.record))
	if err != nil {
		return View{}, err
	}
	s.record.Config = change.Config
	s.record.Revision++
	for name, secret := range change.Secrets {
		s.record.Secrets[name] = secret
	}
	s.receipts[key] = struct {
		hash string
		view View
	}{w.PayloadHash, change.After}
	s.writes++
	return change.After, nil
}

func baseline(role string) config.Config {
	c := config.Config{Environment: "production", HTTPAddr: ":8088", WorkerHTTPAddr: ":8089", PulseDBDSN: "unused", RedisAddr: "unused", IngestBatchSize: 250, SettlementBatchSize: 100, PeriodCloseBatchSize: 20, ContentIngestBatchSize: 100, ContentMaxUserPeriodAmount: 100, ContentMaxDailyAmount: 1000, TicketThresholdMilli: 1000, RewardRandomSecret: strings.Repeat("random", 8), RewardShadowMode: true, QuotaPerUnit: 500000, NewAPIInternalURL: "http://new-api:3000", NewAPILogDSN: "unused"}
	if role == RoleAPI {
		c.ForumHMACSecret = strings.Repeat("forum", 8)
		c.UserBFFHMACSecret = strings.Repeat("user", 10)
		c.AdminHMACSecret = strings.Repeat("admin", 8)
		c.CommunityBFFHMACSecret = strings.Repeat("community", 5)
		c.RollbackHMACSecret = strings.Repeat("rollback", 5)
	} else {
		c.ServiceHMACSecret = strings.Repeat("service", 6)
	}
	return c
}

func managers(t *testing.T) (*Manager, *Manager, *memoryStore) {
	t.Helper()
	ctx := context.Background()
	store := newMemoryStore()
	api, err := New(ctx, store, RoleAPI, t.TempDir(), baseline(RoleAPI))
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(ctx, store, RoleWorker, t.TempDir(), baseline(RoleWorker))
	if err != nil {
		t.Fatal(err)
	}
	return api, worker, store
}

func TestSettingsAreEncryptedSeparatedAndRedacted(t *testing.T) {
	api, worker, store := managers(t)
	ctx := context.Background()
	view, err := api.View(ctx)
	if err != nil {
		t.Fatal(err)
	}
	apiSecret := strings.Repeat("new-admin-", 5)
	workerSecret := strings.Repeat("new-service-", 5)
	view, err = api.Update(ctx, UpdateRequest{Revision: view.Revision, Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": apiSecret, "PULSE_SERVICE_HMAC_SECRET": workerSecret}, Reason: "replace signing credentials"}, "operator", "settings-1")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	for _, secret := range []string{apiSecret, workerSecret, fingerprint(apiSecret), fingerprint(workerSecret)} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("secret material appeared in view")
		}
	}
	for name, secret := range store.record.Secrets {
		if bytes.Contains(secret.Ciphertext, []byte(apiSecret)) || bytes.Contains(secret.Ciphertext, []byte(workerSecret)) {
			t.Fatalf("plaintext persisted in %s", name)
		}
	}
	a, err := api.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	w, err := worker.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if a.AdminHMACSecret != apiSecret || a.ServiceHMACSecret != "" {
		t.Fatal("API secret isolation failed")
	}
	if w.ServiceHMACSecret != workerSecret || w.AdminHMACSecret != "" || w.RollbackHMACSecret != "" || w.CommunityBFFHMACSecret != "" {
		t.Fatal("worker secret isolation failed")
	}
	if _, err := unseal(api.private, "PULSE_SERVICE_HMAC_SECRET", store.record.Secrets["PULSE_SERVICE_HMAC_SECRET"].Ciphertext); err == nil {
		t.Fatal("API decrypted worker envelope")
	}
	if _, err := unseal(api.private, "PULSE_USER_BFF_HMAC_SECRET", store.record.Secrets["PULSE_ADMIN_HMAC_SECRET"].Ciphertext); err == nil {
		t.Fatal("envelope was not bound to field")
	}
	if view.Secrets["PULSE_SERVICE_HMAC_SECRET"].Source != "database" || !view.WorkerReady {
		t.Fatal("incorrect safe status")
	}
}

func TestSettingsReplay100TimesAndConflict(t *testing.T) {
	api, _, store := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	actions := true
	shadow := false
	request := UpdateRequest{Revision: view.Revision, Config: &Patch{ActionsEnabled: &actions, RewardShadowMode: &shadow}, Reason: "open reviewed activity"}
	first, err := api.Update(ctx, request, "operator", "stable-key")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := api.Update(ctx, request, "operator", "stable-key")
			if err != nil {
				errs <- err
			} else if !reflect.DeepEqual(got, first) {
				errs <- errors.New("replay changed response")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if store.writes != 1 {
		t.Fatalf("writes=%d", store.writes)
	}
	request.Reason = "different payload"
	if _, err := api.Update(ctx, request, "operator", "stable-key"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same-key changed payload accepted: %v", err)
	}
	if _, err := api.Update(ctx, request, "operator", "new-key"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
}

func TestSettingsConcurrentCASAllowsOneWriter(t *testing.T) {
	api, _, store := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	var wg sync.WaitGroup
	success := make(chan struct{}, 20)
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			quota := strconvString(500000 + int64(i))
			_, err := api.Update(ctx, UpdateRequest{Revision: view.Revision, Config: &Patch{QuotaPerUnit: &quota}, Reason: "update display conversion"}, "operator", fmt.Sprintf("request-%d", i))
			if err == nil {
				success <- struct{}{}
			} else if !errors.Is(err, ErrConflict) {
				t.Errorf("unexpected CAS error: %v", err)
			}
		}(i)
	}
	wg.Wait()
	close(success)
	if len(success) != 1 || store.writes != 1 {
		t.Fatalf("success=%d writes=%d", len(success), store.writes)
	}
}

func strconvString(value int64) string { return fmt.Sprintf("%d", value) }

func TestSettingsRejectInvalidSecretsAndCrossRoleReuse(t *testing.T) {
	api, _, _ := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	cases := []UpdateRequest{
		{Secrets: map[string]string{"PULSE_SERVICE_HMAC_SECRET": baseline(RoleAPI).AdminHMACSecret}},
		{Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": baseline(RoleWorker).ServiceHMACSecret}},
		{Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": baseline(RoleAPI).RewardRandomSecret}},
		{Secrets: map[string]string{randomSecretName: strings.Repeat("new-random", 5)}},
		{Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": "short"}},
		{Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": strings.Repeat("•", 50)}},
		{ClearSecrets: []string{"PULSE_ADMIN_HMAC_SECRET"}},
		{ClearSecrets: []string{"PULSE_SERVICE_HMAC_SECRET"}},
		{ClearSecrets: []string{"PULSE_ADMIN_HMAC_SECRET_PREVIOUS", "PULSE_ADMIN_HMAC_SECRET_PREVIOUS"}},
		{Secrets: map[string]string{"PULSE_ROLLBACK_HMAC_SECRET": strings.Repeat("new-roll", 5)}, ClearSecrets: []string{"PULSE_ROLLBACK_HMAC_SECRET"}},
	}
	for i, request := range cases {
		request.Revision = view.Revision
		request.Reason = "test validation"
		if _, err := api.Update(ctx, request, "operator", fmt.Sprintf("invalid-%d", i)); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d error=%v", i, err)
		}
	}
}

func TestEmptyKeepsSecretAndExplicitClearDoesNotRestoreEnvironment(t *testing.T) {
	api, _, _ := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	quota := "500001"
	view, err := api.Update(ctx, UpdateRequest{Revision: view.Revision, Config: &Patch{QuotaPerUnit: &quota}, Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": "  "}, ClearSecrets: []string{"PULSE_ROLLBACK_HMAC_SECRET"}, Reason: "disable rollback until configured"}, "operator", "clear-1")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := api.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdminHMACSecret != baseline(RoleAPI).AdminHMACSecret || cfg.RollbackHMACSecret != "" {
		t.Fatal("blank/clear semantics incorrect")
	}
	if view.Secrets["PULSE_ROLLBACK_HMAC_SECRET"].Configured || view.Secrets["PULSE_ROLLBACK_HMAC_SECRET"].Source != "database" {
		t.Fatal("explicit empty override lost")
	}
}

func TestInvalidStoredSettingsFailClosed(t *testing.T) {
	api, _, store := managers(t)
	ctx := context.Background()
	store.failure = errors.New("database unavailable")
	if _, err := api.Current(ctx); err == nil {
		t.Fatal("read failure fell back to environment")
	}
	store.failure = nil
	bad := "not-an-integer"
	store.record.Config.QuotaPerUnit = &bad
	if _, err := api.Current(ctx); err == nil {
		t.Fatal("corrupt configuration fell back")
	}
	store.record.Config = Patch{}
	store.record.Roles[RoleWorker].EnvironmentFingerprints[randomSecretName] = fingerprint("different-seed")
	if _, err := api.Current(ctx); err == nil {
		t.Fatal("different random seed accepted")
	}
}

func TestReceiverURLCannotMoveAfterWorkerRegistration(t *testing.T) {
	api, _, _ := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	other := "http://different-new-api:3000"
	if _, err := api.Update(ctx, UpdateRequest{Revision: view.Revision, Config: &Patch{NewAPIInternalBaseURL: &other}, Reason: "change receiver"}, "operator", "url-1"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("receiver change accepted: %v", err)
	}
	same := "http://new-api:3000/"
	if _, err := api.Update(ctx, UpdateRequest{Revision: view.Revision, Config: &Patch{NewAPIInternalBaseURL: &same}, Reason: "persist existing receiver"}, "operator", "url-2"); err != nil {
		t.Fatal(err)
	}
}

func TestPatchRejectsNonRootURLsAndUnsafeNumbers(t *testing.T) {
	for _, value := range []string{"http://user:secret@host", "http://host/path", "http://host?", "http://host?x=y", "http://host#x", "file:///tmp/config", "https://host/%2F", " https://host", "http://"} {
		if err := validatePatch(&Patch{NewAPIInternalBaseURL: &value}); err == nil {
			t.Errorf("URL accepted: %q", value)
		}
	}
	for _, value := range []string{"0", "-1", "+5", "01", "1.0", "500000.1", "9007199254740992", " 500000"} {
		if err := validatePatch(&Patch{QuotaPerUnit: &value}); err == nil {
			t.Errorf("number accepted: %q", value)
		}
	}
}

func TestRolePrivateKeyPersistsAndRejectsInsecureFiles(t *testing.T) {
	dir := t.TempDir()
	first, err := loadPrivateKey(dir, RoleAPI)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadPrivateKey(dir, RoleAPI)
	if err != nil || !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("key changed across restart")
	}
	if err := os.Chmod(filepath.Join(dir, "api.key"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPrivateKey(dir, RoleAPI); err == nil {
		t.Fatal("publicly readable private key accepted")
	}
}

func TestEnvelopeTamperingAndKeyLossFailClosed(t *testing.T) {
	api, _, store := managers(t)
	ctx := context.Background()
	view, _ := api.View(ctx)
	_, err := api.Update(ctx, UpdateRequest{Revision: view.Revision, Secrets: map[string]string{"PULSE_ADMIN_HMAC_SECRET": strings.Repeat("new-admin", 5)}, Reason: "test persistence"}, "operator", "save-1")
	if err != nil {
		t.Fatal(err)
	}
	secret := store.record.Secrets["PULSE_ADMIN_HMAC_SECRET"]
	secret.Ciphertext[len(secret.Ciphertext)-1] ^= 1
	store.record.Secrets["PULSE_ADMIN_HMAC_SECRET"] = secret
	if _, err := api.Current(ctx); err == nil {
		t.Fatal("tampered envelope accepted")
	}
	if _, err := New(ctx, store, RoleAPI, t.TempDir(), baseline(RoleAPI)); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("lost private key replaced: %v", err)
	}
}

func TestInvalidBootstrapDoesNotPersistAnUnrepairableEnvironmentBaseline(t *testing.T) {
	for _, invalid := range []string{"", "short"} {
		store := newMemoryStore()
		cfg := baseline(RoleAPI)
		cfg.AdminHMACSecret = invalid
		directory := t.TempDir()
		if _, err := New(context.Background(), store, RoleAPI, directory, cfg); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad bootstrap error=%v", err)
		}
		if len(store.record.Roles) != 0 || store.record.Revision != 0 {
			t.Fatal("failed bootstrap persisted a baseline")
		}
		if _, err := New(context.Background(), store, RoleAPI, directory, baseline(RoleAPI)); err != nil {
			t.Fatalf("valid environment repair rejected: %v", err)
		}
	}
}
