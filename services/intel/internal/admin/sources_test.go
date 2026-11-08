package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/settings"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/source/llmstats"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

type memSources struct {
	st  mysqlstore.SourceState
	has bool
}

func (m *memSources) SourceState(context.Context, string) (mysqlstore.SourceState, bool, error) {
	return m.st, m.has, nil
}
func (m *memSources) SaveSourceKey(_ context.Context, _ string, enc []byte, hint, keyID, actor string) error {
	m.has, m.st.Enabled, m.st.KeyEnc, m.st.KeyHint, m.st.KeyID, m.st.UpdatedBy = true, true, enc, hint, keyID, actor
	return nil
}
func (m *memSources) ClearSourceKey(_ context.Context, _ string, actor string) error {
	m.st.KeyEnc, m.st.KeyHint, m.st.KeyID, m.st.Enabled, m.st.UpdatedBy = nil, "", "", false, actor
	return nil
}
func (m *memSources) SetSourceEnabled(_ context.Context, _ string, enabled bool, actor string) error {
	m.has, m.st.Enabled, m.st.UpdatedBy = true, enabled, actor
	return nil
}
func (m *memSources) RequestSourceSync(_ context.Context, _ string, _ string, now time.Time) error {
	m.has, m.st.SyncRequestedAt = true, &now
	return nil
}

const goodKey = "sk_ze_live_abcdefgh1234"

func sources(t *testing.T, verify func(context.Context, string, string) error) (*Sources, *memSources) {
	t.Helper()
	box, err := settings.NewBox([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	store := &memSources{}
	return &Sources{Store: store, Box: box, SyncInterval: 24 * time.Hour, Verify: verify}, store
}

func ptr[T any](v T) *T { return &v }

func TestSaveKeyVerifiesEncryptsAndRequestsSync(t *testing.T) {
	verified := ""
	s, store := sources(t, func(_ context.Context, _, key string) error { verified = key; return nil })
	v, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr("  " + goodKey + " ")})
	if err != nil {
		t.Fatal(err)
	}
	if verified != goodKey {
		t.Fatalf("key not verified before saving: %q", verified)
	}
	if strings.Contains(string(store.st.KeyEnc), goodKey) {
		t.Fatal("key stored in plaintext")
	}
	if store.st.SyncRequestedAt == nil {
		t.Fatal("a new key must request an immediate sync")
	}
	l := v.LLMStats
	if !l.Configured || l.Origin != llmstats.OriginDatabase || l.KeyHint != "1234" || l.KeyStale || !l.CanEdit || l.UpdatedBy != "7" {
		t.Fatalf("view %+v", l)
	}
	raw, _ := json.Marshal(v)
	if strings.Contains(string(raw), goodKey) || strings.Contains(string(raw), "live_abcd") {
		t.Fatal("view leaks the key")
	}
	key, origin, err := llmstats.EffectiveKey(context.Background(), store, s.Box, "")
	if err != nil || key != goodKey || origin != llmstats.OriginDatabase {
		t.Fatalf("effective key %q %q %v", key, origin, err)
	}
}

func TestRejectsBadKeysWithoutSaving(t *testing.T) {
	s, store := sources(t, func(context.Context, string, string) error {
		return &llmstats.APIError{Status: 401, Code: "invalid_api_key"}
	})
	if _, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr(goodKey)}); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("rejected key: %v", err)
	}
	if _, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr("not-a-key")}); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("malformed key: %v", err)
	}
	if store.has {
		t.Fatal("nothing may be stored for a bad key")
	}
	s.Verify = func(context.Context, string, string) error { return errors.New("network down") }
	if _, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr(goodKey)}); !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("unverifiable key: %v", err)
	}
	if _, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr(goodKey), Clear: true}); !errors.Is(err, ErrInvalidSourceReq) {
		t.Fatal("two actions at once must be rejected")
	}
}

func TestEnvKeyIsReadOnlyAndWins(t *testing.T) {
	s, store := sources(t, func(context.Context, string, string) error { return nil })
	s.EnvKey = "sk_ze_from_environment_9999"
	if _, err := s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{Clear: true}); !errors.Is(err, ErrManagedByEnv) {
		t.Fatalf("env key editable: %v", err)
	}
	v, _ := s.View(context.Background())
	if v.LLMStats.Origin != llmstats.OriginEnv || v.LLMStats.CanEdit || v.LLMStats.KeyHint != "9999" {
		t.Fatalf("view %+v", v.LLMStats)
	}
	if _, err := s.RequestSync(context.Background(), "7"); err != nil || store.st.SyncRequestedAt == nil {
		t.Fatalf("env-configured sync request: %v", err)
	}
}

func TestDisableClearAndStaleSecret(t *testing.T) {
	s, store := sources(t, func(context.Context, string, string) error { return nil })
	if _, err := s.RequestSync(context.Background(), "7"); !errors.Is(err, ErrNotConfigured) {
		t.Fatal("sync without a key must be refused")
	}
	s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{APIKey: ptr(goodKey)})
	s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{Enabled: ptr(false)})
	if key, origin, _ := llmstats.EffectiveKey(context.Background(), store, s.Box, ""); key != "" || origin != llmstats.OriginDisabled {
		t.Fatalf("disabled source still has a key: %q %q", key, origin)
	}
	s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{Enabled: ptr(true)})
	rotated, _ := settings.NewBox([]byte(strings.Repeat("b", 32)))
	s.Box = rotated
	v, _ := s.View(context.Background())
	if !v.LLMStats.KeyStale {
		t.Fatal("a rotated admin secret must mark the stored key stale")
	}
	if _, _, err := llmstats.EffectiveKey(context.Background(), store, rotated, ""); !errors.Is(err, settings.ErrStale) {
		t.Fatalf("stale key decrypted: %v", err)
	}
	s.UpdateLLMStats(context.Background(), "7", LLMStatsUpdate{Clear: true})
	if v, _ := s.View(context.Background()); v.LLMStats.Configured {
		t.Fatal("clear must remove the key")
	}
}
