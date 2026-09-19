package runtimeconfig

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nanashiwang/meta-pulse/internal/config"
)

const randomSecretName = "PULSE_REWARD_RANDOM_SECRET"

var secretNames = []string{
	"PULSE_FORUM_HMAC_SECRET", "PULSE_FORUM_HMAC_SECRET_PREVIOUS",
	"PULSE_USER_BFF_HMAC_SECRET", "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS",
	"PULSE_ADMIN_HMAC_SECRET", "PULSE_ADMIN_HMAC_SECRET_PREVIOUS",
	"PULSE_COMMUNITY_BFF_HMAC_SECRET", "PULSE_COMMUNITY_BFF_HMAC_SECRET_PREVIOUS",
	"PULSE_ROLLBACK_HMAC_SECRET", "PULSE_ROLLBACK_HMAC_SECRET_PREVIOUS",
	"PULSE_SERVICE_HMAC_SECRET", "PULSE_SERVICE_HMAC_SECRET_PREVIOUS",
}

// SecretRole is an explicit allowlist: unknown configuration keys are never
// accepted, even if they resemble a supported environment variable.
func SecretRole(name string) string {
	for _, allowed := range secretNames {
		if name == allowed {
			if strings.HasPrefix(name, "PULSE_SERVICE_") {
				return RoleWorker
			}
			return RoleAPI
		}
	}
	return ""
}

type Manager struct {
	store    Store
	role     string
	private  *ecdh.PrivateKey
	baseline config.Config
}

func New(ctx context.Context, store Store, role, keyDir string, baseline config.Config) (*Manager, error) {
	if store == nil || (role != RoleAPI && role != RoleWorker) {
		return nil, ErrInvalid
	}
	// Reject malformed supplied credentials before persisting a role baseline.
	// Missing credentials may legitimately be supplied by encrypted DB values.
	for name, value := range configSecrets(baseline) {
		if SecretRole(name) != role && name != randomSecretName {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" && baseline.Environment == "production" && (len(value) < 32 || value == "replace-me") {
			return nil, fmt.Errorf("%w: %s must have at least 32 bytes in production", ErrInvalid, name)
		}
	}
	private, err := loadPrivateKey(keyDir, role)
	if err != nil {
		return nil, err
	}
	environment := map[string]string{}
	for name, secret := range configSecrets(baseline) {
		if SecretRole(name) == role || name == randomSecretName {
			environment[name] = fingerprint(strings.TrimSpace(secret))
		}
	}
	if err := store.Register(ctx, Registration{Role: role, PublicKey: private.PublicKey().Bytes(), EnvironmentFingerprints: environment, NewAPIInternalBaseURL: baseline.NewAPIInternalURL}); err != nil {
		return nil, err
	}
	return &Manager{store: store, role: role, private: private, baseline: baseline}, nil
}

func (m *Manager) Current(ctx context.Context) (config.Config, error) {
	record, err := m.store.Read(ctx, m.role)
	if err != nil {
		return config.Config{}, err
	}
	return m.current(record)
}

func (m *Manager) current(record Record) (config.Config, error) {
	if err := ValidateRecord(record); err != nil {
		return config.Config{}, err
	}
	registered, exists := record.Roles[m.role]
	if !exists || !reflect.DeepEqual(registered.PublicKey, m.private.PublicKey().Bytes()) {
		return config.Config{}, ErrKeyMismatch
	}
	expected := map[string]string{}
	for name, value := range configSecrets(m.baseline) {
		if SecretRole(name) == m.role || name == randomSecretName {
			expected[name] = fingerprint(strings.TrimSpace(value))
		}
	}
	if !reflect.DeepEqual(expected, registered.EnvironmentFingerprints) {
		return config.Config{}, fmt.Errorf("%w: process environment differs from registered role", ErrConflict)
	}
	cfg := m.baseline
	applyPatch(&cfg, record.Config)
	for _, name := range secretNames {
		if SecretRole(name) != m.role {
			setSecret(&cfg, name, "")
			continue
		}
		stored, ok := record.Secrets[name]
		if !ok {
			continue
		}
		value := ""
		if stored.Fingerprint != "" {
			var err error
			value, err = unseal(m.private, name, stored.Ciphertext)
			if err != nil {
				return config.Config{}, err
			}
			if fingerprint(value) != stored.Fingerprint {
				return config.Config{}, fmt.Errorf("%w: runtime secret integrity", ErrInvalid)
			}
		}
		setSecret(&cfg, name, value)
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return cfg, nil
}

func (m *Manager) View(ctx context.Context) (View, error) {
	record, err := m.store.Read(ctx, m.role)
	if err != nil {
		return View{}, err
	}
	if _, err := m.current(record); err != nil {
		return View{}, err
	}
	return m.view(record), nil
}

func (m *Manager) view(record Record) View {
	cfg := m.baseline
	applyPatch(&cfg, record.Config)
	view := View{Revision: record.Revision, Config: PublicConfig{
		NewAPIInternalBaseURL: cfg.NewAPIInternalURL, QuotaPerUnit: strconv.FormatInt(cfg.QuotaPerUnit, 10),
		ActionsEnabled: cfg.ActionsEnabled, RewardShadowMode: cfg.RewardShadowMode,
	}, Secrets: map[string]SecretStatus{}}
	for _, name := range secretNames {
		status := SecretStatus{Source: "unset"}
		if secret, ok := record.Secrets[name]; ok {
			status.Source, status.Configured = "database", secret.Fingerprint != ""
		} else if effectiveFingerprint(record, name) != "" {
			status.Source, status.Configured = "environment", true
		}
		view.Secrets[name] = status
	}
	worker, exists := record.Roles[RoleWorker]
	view.WorkerReady = exists && len(worker.PublicKey) == 32 && view.Secrets["PULSE_SERVICE_HMAC_SECRET"].Configured
	view.NewAPITargetLocked = exists || record.HasRewards
	return view
}

func (m *Manager) Update(ctx context.Context, request UpdateRequest, actorID, requestID string) (View, error) {
	if m.role != RoleAPI {
		return View{}, fmt.Errorf("%w: only API manages settings", ErrInvalid)
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if !validText(request.Reason, 500) || !validText(actorID, 128) || !validText(requestID, 128) {
		return View{}, fmt.Errorf("%w: actor, idempotency key and reason are required", ErrInvalid)
	}
	if err := validatePatch(request.Config); err != nil {
		return View{}, err
	}
	updates := map[string]string{}
	for name, value := range request.Secrets {
		if SecretRole(name) == "" {
			return View{}, fmt.Errorf("%w: unsupported secret field", ErrInvalid)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		} // Password placeholders never erase a secret.
		if !validSecret(value) {
			return View{}, fmt.Errorf("%w: %s must contain 32-1024 printable ASCII bytes", ErrInvalid, name)
		}
		updates[name] = value
	}
	clears := map[string]bool{}
	for _, name := range request.ClearSecrets {
		if SecretRole(name) == "" || clears[name] || updates[name] != "" {
			return View{}, fmt.Errorf("%w: invalid secret clearing request", ErrInvalid)
		}
		clears[name] = true
	}
	request.Secrets = updates
	request.ClearSecrets = nil
	for name := range clears {
		request.ClearSecrets = append(request.ClearSecrets, name)
	}
	sort.Strings(request.ClearSecrets)
	payload, err := json.Marshal(request)
	if err != nil {
		return View{}, ErrInvalid
	}
	hash := sha256.Sum256(payload)
	write := WriteRequest{Revision: request.Revision, ActorID: actorID, RequestID: requestID, PayloadHash: hex.EncodeToString(hash[:]), Reason: request.Reason}
	return m.store.Update(ctx, write, func(record Record) (Mutation, error) {
		if _, err := m.current(record); err != nil {
			return Mutation{}, err
		}
		before := m.view(record)
		candidate := cloneRecord(record)
		changes := Mutation{Config: mergePatch(candidate.Config, request.Config), Secrets: map[string]Secret{}, Before: before}
		candidate.Config = changes.Config
		if request.Config != nil && request.Config.NewAPIInternalBaseURL != nil && strings.TrimRight(*request.Config.NewAPIInternalBaseURL, "/") != strings.TrimRight(before.Config.NewAPIInternalBaseURL, "/") {
			actions := before.Config.ActionsEnabled
			if request.Config.ActionsEnabled != nil {
				actions = actions || *request.Config.ActionsEnabled
			}
			_, workerRegistered := candidate.Roles[RoleWorker]
			if record.HasRewards || actions || workerRegistered {
				return Mutation{}, fmt.Errorf("%w: changing the funds receiver after worker registration requires an offline maintenance migration", ErrInvalid)
			}
		}
		if request.Config != nil {
			if request.Config.NewAPIInternalBaseURL != nil {
				changes.ChangedFields = append(changes.ChangedFields, "newapi_internal_base_url")
			}
			if request.Config.QuotaPerUnit != nil {
				changes.ChangedFields = append(changes.ChangedFields, "quota_per_unit")
			}
			if request.Config.ActionsEnabled != nil {
				changes.ChangedFields = append(changes.ChangedFields, "actions_enabled")
			}
			if request.Config.RewardShadowMode != nil {
				changes.ChangedFields = append(changes.ChangedFields, "reward_shadow_mode")
			}
		}
		for name, value := range updates {
			role := SecretRole(name)
			registration, ok := record.Roles[role]
			if !ok {
				return Mutation{}, fmt.Errorf("%w: %s has not registered its encryption key", ErrInvalid, role)
			}
			encrypted, err := seal(registration.PublicKey, name, value)
			if err != nil {
				return Mutation{}, err
			}
			secret := Secret{Role: role, Ciphertext: encrypted, Fingerprint: fingerprint(value)}
			candidate.Secrets[name], changes.Secrets[name] = secret, secret
			changes.ChangedFields = append(changes.ChangedFields, name)
		}
		for name := range clears {
			secret := Secret{Role: SecretRole(name)}
			candidate.Secrets[name], changes.Secrets[name] = secret, secret
			changes.ChangedFields = append(changes.ChangedFields, name)
		}
		if len(changes.ChangedFields) == 0 {
			return Mutation{}, fmt.Errorf("%w: no settings supplied", ErrInvalid)
		}
		if err := ValidateRecord(candidate); err != nil {
			return Mutation{}, err
		}
		cfg, err := m.current(candidate)
		if err != nil {
			return Mutation{}, err
		}
		if err := cfg.ValidateAPI(); err != nil {
			return Mutation{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if _, registered := candidate.Roles[RoleWorker]; registered && effectiveFingerprint(candidate, "PULSE_SERVICE_HMAC_SECRET") == "" {
			return Mutation{}, fmt.Errorf("%w: worker settlement secret is required", ErrInvalid)
		}
		if cfg.ActionsEnabled && !cfg.RewardShadowMode {
			if cfg.NewAPIInternalURL == "" || effectiveFingerprint(candidate, "PULSE_COMMUNITY_BFF_HMAC_SECRET") == "" || !m.view(candidate).WorkerReady {
				return Mutation{}, fmt.Errorf("%w: live actions require new-api URL, community secret and configured worker", ErrInvalid)
			}
		}
		candidate.Revision++
		changes.After = m.view(candidate)
		sort.Strings(changes.ChangedFields)
		return changes, nil
	})
}

func validatePatch(patch *Patch) error {
	if patch == nil {
		return nil
	}
	if patch.NewAPIInternalBaseURL != nil {
		raw := *patch.NewAPIInternalBaseURL
		parsed, err := url.Parse(raw)
		if err != nil || strings.TrimSpace(raw) != raw || len(raw) > 2048 || (raw != "" && ((parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.RawPath != "" && parsed.RawPath != "/"))) {
			return fmt.Errorf("%w: new-api URL must be an HTTP(S) root without credentials, query or fragment", ErrInvalid)
		}
	}
	if patch.QuotaPerUnit != nil {
		value, err := strconv.ParseInt(*patch.QuotaPerUnit, 10, 64)
		if err != nil || value <= 0 || value > MaxSafeInteger || strconv.FormatInt(value, 10) != *patch.QuotaPerUnit {
			return fmt.Errorf("%w: quota_per_unit must be a positive integer no larger than 2^53-1", ErrInvalid)
		}
	}
	return nil
}

// ValidateRecord applies across both roles using fingerprints only. A random
// seed is shared by API/Worker but remains excluded from all editable fields.
func ValidateRecord(record Record) error {
	if err := validatePatch(&record.Config); err != nil {
		return err
	}
	for role, required := range map[string][]string{
		RoleAPI:    {"PULSE_FORUM_HMAC_SECRET", "PULSE_USER_BFF_HMAC_SECRET", "PULSE_ADMIN_HMAC_SECRET"},
		RoleWorker: {"PULSE_SERVICE_HMAC_SECRET"},
	} {
		registration, exists := record.Roles[role]
		if !exists {
			continue
		}
		if registration.EnvironmentFingerprints[randomSecretName] == "" {
			return fmt.Errorf("%w: reward random seed is required before registering %s", ErrInvalid, role)
		}
		for _, name := range required {
			if effectiveFingerprint(record, name) == "" {
				return fmt.Errorf("%w: %s must be configured before registering %s", ErrInvalid, name, role)
			}
		}
	}
	if api, exists := record.Roles[RoleAPI]; exists {
		if worker, exists := record.Roles[RoleWorker]; exists && api.EnvironmentFingerprints[randomSecretName] != worker.EnvironmentFingerprints[randomSecretName] {
			return fmt.Errorf("%w: reward random seed differs between process roles", ErrInvalid)
		}
	}
	owners := map[string]string{}
	allNames := append(append([]string{}, secretNames...), randomSecretName)
	for _, name := range allNames {
		value := effectiveFingerprint(record, name)
		if value == "" {
			continue
		}
		if len(value) != 64 {
			return fmt.Errorf("%w: invalid secret metadata", ErrInvalid)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return fmt.Errorf("%w: invalid secret metadata", ErrInvalid)
		}
		if owner, ok := owners[value]; ok {
			return fmt.Errorf("%w: %s must not reuse %s", ErrInvalid, name, owner)
		}
		owners[value] = name
		if strings.HasSuffix(name, "_PREVIOUS") && effectiveFingerprint(record, strings.TrimSuffix(name, "_PREVIOUS")) == "" {
			return fmt.Errorf("%w: previous secret requires its current secret", ErrInvalid)
		}
	}
	for name, secret := range record.Secrets {
		if SecretRole(name) == "" || SecretRole(name) != secret.Role {
			return fmt.Errorf("%w: invalid secret role", ErrInvalid)
		}
	}
	return nil
}

func effectiveFingerprint(record Record, name string) string {
	if secret, ok := record.Secrets[name]; ok {
		return secret.Fingerprint
	}
	role := SecretRole(name)
	if name == randomSecretName {
		role = RoleAPI
		if _, ok := record.Roles[role]; !ok {
			role = RoleWorker
		}
	}
	return record.Roles[role].EnvironmentFingerprints[name]
}

func validText(value string, max int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}
func validSecret(value string) bool {
	if len(value) < 32 || len(value) > 1024 || value == "replace-me" {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}

func cloneRecord(record Record) Record {
	copy := record
	copy.Secrets = make(map[string]Secret, len(record.Secrets))
	for name, secret := range record.Secrets {
		copy.Secrets[name] = secret
	}
	return copy
}

func mergePatch(current Patch, next *Patch) Patch {
	if next == nil {
		return current
	}
	if next.NewAPIInternalBaseURL != nil {
		current.NewAPIInternalBaseURL = next.NewAPIInternalBaseURL
	}
	if next.QuotaPerUnit != nil {
		current.QuotaPerUnit = next.QuotaPerUnit
	}
	if next.ActionsEnabled != nil {
		current.ActionsEnabled = next.ActionsEnabled
	}
	if next.RewardShadowMode != nil {
		current.RewardShadowMode = next.RewardShadowMode
	}
	return current
}

func applyPatch(cfg *config.Config, patch Patch) {
	if patch.NewAPIInternalBaseURL != nil {
		cfg.NewAPIInternalURL = *patch.NewAPIInternalBaseURL
	}
	if patch.QuotaPerUnit != nil {
		cfg.QuotaPerUnit, _ = strconv.ParseInt(*patch.QuotaPerUnit, 10, 64)
	}
	if patch.ActionsEnabled != nil {
		cfg.ActionsEnabled = *patch.ActionsEnabled
	}
	if patch.RewardShadowMode != nil {
		cfg.RewardShadowMode = *patch.RewardShadowMode
	}
}

func configSecrets(cfg config.Config) map[string]string {
	return map[string]string{
		"PULSE_FORUM_HMAC_SECRET": cfg.ForumHMACSecret, "PULSE_FORUM_HMAC_SECRET_PREVIOUS": cfg.ForumHMACSecretPrevious,
		"PULSE_USER_BFF_HMAC_SECRET": cfg.UserBFFHMACSecret, "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS": cfg.UserBFFHMACSecretPrevious,
		"PULSE_ADMIN_HMAC_SECRET": cfg.AdminHMACSecret, "PULSE_ADMIN_HMAC_SECRET_PREVIOUS": cfg.AdminHMACSecretPrevious,
		"PULSE_COMMUNITY_BFF_HMAC_SECRET": cfg.CommunityBFFHMACSecret, "PULSE_COMMUNITY_BFF_HMAC_SECRET_PREVIOUS": cfg.CommunityBFFHMACSecretPrevious,
		"PULSE_ROLLBACK_HMAC_SECRET": cfg.RollbackHMACSecret, "PULSE_ROLLBACK_HMAC_SECRET_PREVIOUS": cfg.RollbackHMACSecretPrevious,
		"PULSE_SERVICE_HMAC_SECRET": cfg.ServiceHMACSecret, "PULSE_SERVICE_HMAC_SECRET_PREVIOUS": cfg.ServiceHMACSecretPrevious,
		randomSecretName: cfg.RewardRandomSecret,
	}
}

func setSecret(cfg *config.Config, name, value string) {
	switch name {
	case "PULSE_FORUM_HMAC_SECRET":
		cfg.ForumHMACSecret = value
	case "PULSE_FORUM_HMAC_SECRET_PREVIOUS":
		cfg.ForumHMACSecretPrevious = value
	case "PULSE_USER_BFF_HMAC_SECRET":
		cfg.UserBFFHMACSecret = value
	case "PULSE_USER_BFF_HMAC_SECRET_PREVIOUS":
		cfg.UserBFFHMACSecretPrevious = value
	case "PULSE_ADMIN_HMAC_SECRET":
		cfg.AdminHMACSecret = value
	case "PULSE_ADMIN_HMAC_SECRET_PREVIOUS":
		cfg.AdminHMACSecretPrevious = value
	case "PULSE_COMMUNITY_BFF_HMAC_SECRET":
		cfg.CommunityBFFHMACSecret = value
	case "PULSE_COMMUNITY_BFF_HMAC_SECRET_PREVIOUS":
		cfg.CommunityBFFHMACSecretPrevious = value
	case "PULSE_ROLLBACK_HMAC_SECRET":
		cfg.RollbackHMACSecret = value
	case "PULSE_ROLLBACK_HMAC_SECRET_PREVIOUS":
		cfg.RollbackHMACSecretPrevious = value
	case "PULSE_SERVICE_HMAC_SECRET":
		cfg.ServiceHMACSecret = value
	case "PULSE_SERVICE_HMAC_SECRET_PREVIOUS":
		cfg.ServiceHMACSecretPrevious = value
	}
}
