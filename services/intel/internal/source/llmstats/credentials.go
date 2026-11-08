package llmstats

import (
	"context"
	"errors"
	"regexp"

	"github.com/nanashiwang/meta-pulse/services/intel/internal/settings"
	mysqlstore "github.com/nanashiwang/meta-pulse/services/intel/internal/store/mysql"
)

// Key origins shown in the admin UI.
const (
	OriginEnv      = "env"
	OriginDatabase = "database"
	OriginNone     = "none"
	OriginDisabled = "disabled"
)

// KeyPattern accepts current (sk_ze_) and older (ze_) LLM Stats keys.
var KeyPattern = regexp.MustCompile(`^(?:sk_ze_|ze_)[A-Za-z0-9_-]{8,200}$`)

type StateReader interface {
	SourceState(ctx context.Context, source string) (mysqlstore.SourceState, bool, error)
}

// EffectiveKey resolves the key to use. A key in the server environment
// always wins and cannot be changed from the UI; otherwise the encrypted key
// saved from the admin UI is used while the source is enabled.
func EffectiveKey(ctx context.Context, store StateReader, box *settings.Box, envKey string) (string, string, error) {
	if envKey != "" {
		return envKey, OriginEnv, nil
	}
	st, ok, err := store.SourceState(ctx, SourceSlug)
	if err != nil {
		return "", "", err
	}
	if !ok || len(st.KeyEnc) == 0 {
		return "", OriginNone, nil
	}
	if !st.Enabled {
		return "", OriginDisabled, nil
	}
	if box == nil {
		return "", OriginDatabase, settings.ErrUnavailable
	}
	key, err := box.Open(st.KeyEnc, st.KeyID)
	if errors.Is(err, settings.ErrStale) {
		return "", OriginDatabase, err
	}
	return key, OriginDatabase, err
}
