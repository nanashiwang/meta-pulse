package admin

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/settings"
	"github.com/nanashiwang/meta-pulse/services/intel/internal/source/llmstats"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

var (
	ErrManagedByEnv     = errors.New("managed_by_env")
	ErrInvalidAPIKey    = errors.New("invalid_api_key")
	ErrNotConfigured    = errors.New("not_configured")
	ErrVerifyFailed     = errors.New("verify_unavailable")
	ErrInvalidSourceReq = errors.New("invalid_request")
)

type SourceStore interface {
	SourceState(ctx context.Context, source string) (mysqlstore.SourceState, bool, error)
	SaveSourceKey(ctx context.Context, source string, enc []byte, hint, keyID, actor string) error
	ClearSourceKey(ctx context.Context, source, actor string) error
	SetSourceEnabled(ctx context.Context, source string, enabled bool, actor string) error
	RequestSourceSync(ctx context.Context, source, actor string, now time.Time) error
}

// Sources manages data-source credentials. The plaintext key only exists in
// memory while it is verified and sealed; responses carry a 4-char hint.
type Sources struct {
	Store        SourceStore
	Box          *settings.Box
	EnvKey       string
	BaseURL      string
	SyncInterval time.Duration
	Verify       func(ctx context.Context, baseURL, key string) error
	Now          func() time.Time
}

type LLMStatsView struct {
	Origin          string     `json:"origin"`
	Configured      bool       `json:"configured"`
	Enabled         bool       `json:"enabled"`
	KeyHint         string     `json:"key_hint"`
	KeyStale        bool       `json:"key_stale"`
	IntervalHours   int        `json:"interval_hours"`
	UpdatedBy       string     `json:"updated_by"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
	SyncRequestedAt *time.Time `json:"sync_requested_at,omitempty"`
	LastAttemptAt   *time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt   *time.Time `json:"last_success_at,omitempty"`
	LastStatus      string     `json:"last_status"`
	LastError       string     `json:"last_error"`
	LastReport      string     `json:"last_report"`
	LastRequests    int        `json:"last_requests"`
	CanEdit         bool       `json:"can_edit"`
}

type SourcesView struct {
	LLMStats LLMStatsView `json:"llm_stats"`
}

func (s *Sources) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sources) View(ctx context.Context) (SourcesView, error) {
	v := LLMStatsView{IntervalHours: int(s.SyncInterval / time.Hour), CanEdit: s.EnvKey == "" && s.Box != nil}
	st, ok, err := s.Store.SourceState(ctx, llmstats.SourceSlug)
	if err != nil {
		return SourcesView{}, err
	}
	if ok {
		v.Enabled, v.UpdatedBy = st.Enabled, st.UpdatedBy
		u := st.UpdatedAt
		v.UpdatedAt = &u
		v.SyncRequestedAt, v.LastAttemptAt, v.LastSuccessAt = st.SyncRequestedAt, st.LastAttemptAt, st.LastSuccessAt
		v.LastStatus, v.LastError, v.LastReport, v.LastRequests = st.LastStatus, st.LastError, st.LastReport, st.LastRequests
	}
	switch {
	case s.EnvKey != "":
		v.Origin, v.Configured, v.Enabled, v.KeyHint = llmstats.OriginEnv, true, true, settings.Hint(s.EnvKey)
	case ok && len(st.KeyEnc) > 0:
		v.Origin, v.Configured, v.KeyHint = llmstats.OriginDatabase, true, st.KeyHint
		v.KeyStale = s.Box == nil || st.KeyID != s.Box.KeyID()
	default:
		v.Origin = llmstats.OriginNone
	}
	return SourcesView{LLMStats: v}, nil
}

type LLMStatsUpdate struct {
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
	Clear   bool    `json:"clear"`
}

// UpdateLLMStats saves, clears or toggles the key. A new key is verified
// against the quota-free account route before it is stored.
func (s *Sources) UpdateLLMStats(ctx context.Context, actor string, req LLMStatsUpdate) (SourcesView, error) {
	if s.EnvKey != "" {
		return SourcesView{}, ErrManagedByEnv
	}
	if s.Box == nil {
		return SourcesView{}, settings.ErrUnavailable
	}
	actions := 0
	if req.APIKey != nil {
		actions++
	}
	if req.Clear {
		actions++
	}
	if req.Enabled != nil {
		actions++
	}
	if actions != 1 {
		return SourcesView{}, ErrInvalidSourceReq
	}
	switch {
	case req.APIKey != nil:
		key := strings.TrimSpace(*req.APIKey)
		if !llmstats.KeyPattern.MatchString(key) {
			return SourcesView{}, ErrInvalidAPIKey
		}
		vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.Verify(vctx, s.BaseURL, key)
		cancel()
		var apiErr *llmstats.APIError
		if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
			return SourcesView{}, ErrInvalidAPIKey
		}
		if err != nil {
			return SourcesView{}, ErrVerifyFailed
		}
		sealed, err := s.Box.Seal(key)
		if err != nil {
			return SourcesView{}, err
		}
		if err := s.Store.SaveSourceKey(ctx, llmstats.SourceSlug, sealed, settings.Hint(key), s.Box.KeyID(), actor); err != nil {
			return SourcesView{}, err
		}
		// A fresh key syncs right away instead of waiting a full interval.
		if err := s.Store.RequestSourceSync(ctx, llmstats.SourceSlug, actor, s.now()); err != nil {
			return SourcesView{}, err
		}
	case req.Clear:
		if err := s.Store.ClearSourceKey(ctx, llmstats.SourceSlug, actor); err != nil {
			return SourcesView{}, err
		}
	default:
		if err := s.Store.SetSourceEnabled(ctx, llmstats.SourceSlug, *req.Enabled, actor); err != nil {
			return SourcesView{}, err
		}
	}
	return s.View(ctx)
}

func (s *Sources) RequestSync(ctx context.Context, actor string) (SourcesView, error) {
	key, _, err := llmstats.EffectiveKey(ctx, s.Store, s.Box, s.EnvKey)
	if err != nil || key == "" {
		return SourcesView{}, ErrNotConfigured
	}
	if err := s.Store.RequestSourceSync(ctx, llmstats.SourceSlug, actor, s.now()); err != nil {
		return SourcesView{}, err
	}
	return s.View(ctx)
}

// VerifyWithAPI is the production verifier.
func VerifyWithAPI(ctx context.Context, baseURL, key string) error {
	c := llmstats.NewClient(baseURL, key)
	c.Pause = 0
	return c.Account(ctx)
}
