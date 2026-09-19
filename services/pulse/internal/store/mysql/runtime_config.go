package mysql

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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/runtimeconfig"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type runtimeConfigStore struct{ db *gorm.DB }
type runtimeConfigModel struct {
	ID         uint64    `gorm:"column:id;primaryKey"`
	Revision   uint64    `gorm:"column:revision"`
	ConfigJSON []byte    `gorm:"column:config_json"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}

func (runtimeConfigModel) TableName() string { return "pulse_runtime_config" }

type runtimeRoleModel struct {
	Role            string    `gorm:"column:role;primaryKey"`
	PublicKey       []byte    `gorm:"column:public_key"`
	EnvironmentJSON []byte    `gorm:"column:environment_json"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (runtimeRoleModel) TableName() string { return "pulse_runtime_role" }

type runtimeSecretModel struct {
	Name        string    `gorm:"column:secret_name;primaryKey"`
	Role        string    `gorm:"column:role"`
	Ciphertext  []byte    `gorm:"column:ciphertext"`
	Fingerprint string    `gorm:"column:fingerprint"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (runtimeSecretModel) TableName() string { return "pulse_runtime_secret" }

type runtimeMutationModel struct {
	ActorID      string    `gorm:"column:actor_id;primaryKey"`
	RequestHash  string    `gorm:"column:request_hash;primaryKey"`
	PayloadHash  string    `gorm:"column:payload_hash"`
	ResponseJSON []byte    `gorm:"column:response_json"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (runtimeMutationModel) TableName() string { return "pulse_runtime_change" }

func NewRuntimeConfigStore(database *DB) runtimeconfig.Store {
	if database == nil {
		return nil
	}
	return &runtimeConfigStore{db: database.GORM()}
}

func (s *runtimeConfigStore) Register(ctx context.Context, registration runtimeconfig.Registration) error {
	if (registration.Role != runtimeconfig.RoleAPI && registration.Role != runtimeconfig.RoleWorker) || len(registration.PublicKey) != 32 {
		return runtimeconfig.ErrInvalid
	}
	for name, fingerprint := range registration.EnvironmentFingerprints {
		if (runtimeconfig.SecretRole(name) != registration.Role && name != "PULSE_REWARD_RANDOM_SECRET") || (fingerprint != "" && !validLowerHex(fingerprint, 64)) {
			return runtimeconfig.ErrInvalid
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockRuntimeConfig(tx); err != nil {
			return err
		}
		record, err := readRuntimeRecord(tx, registration.Role)
		if err != nil {
			return err
		}
		previous, exists := record.Roles[registration.Role]
		if exists {
			if !reflect.DeepEqual(previous.EnvironmentFingerprints, registration.EnvironmentFingerprints) {
				return fmt.Errorf("%w: role environment differs from its registered baseline; keep replicas consistent and manage signing keys in the console", runtimeconfig.ErrConflict)
			}
			if !bytes.Equal(previous.PublicKey, registration.PublicKey) {
				for _, secret := range record.Secrets {
					if secret.Role == registration.Role && secret.Fingerprint != "" {
						return runtimeconfig.ErrKeyMismatch
					}
				}
			}
		}
		pinReceiver := registration.Role == runtimeconfig.RoleWorker && record.Config.NewAPIInternalBaseURL == nil
		if pinReceiver {
			if strings.TrimSpace(registration.NewAPIInternalBaseURL) == "" {
				return fmt.Errorf("%w: worker requires a new-api receiver URL before registration", runtimeconfig.ErrInvalid)
			}
			url := strings.TrimRight(registration.NewAPIInternalBaseURL, "/")
			record.Config.NewAPIInternalBaseURL = &url
		}
		if registration.Role == runtimeconfig.RoleWorker && record.Config.NewAPIInternalBaseURL != nil && *record.Config.NewAPIInternalBaseURL == "" {
			return fmt.Errorf("%w: worker requires a new-api receiver URL before registration", runtimeconfig.ErrInvalid)
		}
		// The immutable reward seed is intentionally shared between API and
		// Worker. A different seed would invalidate reproducible results.
		for role, other := range record.Roles {
			if role != registration.Role && other.EnvironmentFingerprints["PULSE_REWARD_RANDOM_SECRET"] != registration.EnvironmentFingerprints["PULSE_REWARD_RANDOM_SECRET"] {
				return fmt.Errorf("%w: reward random seed differs between process roles", runtimeconfig.ErrConflict)
			}
		}
		record.Roles[registration.Role] = registration
		if err := runtimeconfig.ValidateRecord(record); err != nil {
			return err
		}
		if exists && bytes.Equal(previous.PublicKey, registration.PublicKey) && !pinReceiver {
			return nil
		}
		encoded, err := json.Marshal(registration.EnvironmentFingerprints)
		if err != nil {
			return runtimeconfig.ErrInvalid
		}
		row := runtimeRoleModel{Role: registration.Role, PublicKey: registration.PublicKey, EnvironmentJSON: encoded, UpdatedAt: time.Now().UTC()}
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row).Error; err != nil {
			return fmt.Errorf("register runtime role: %w", err)
		}
		updates := map[string]any{"revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()}
		if pinReceiver {
			configJSON, err := json.Marshal(record.Config)
			if err != nil {
				return runtimeconfig.ErrInvalid
			}
			updates["config_json"] = configJSON
			before, _ := json.Marshal(map[string]any{"revision": record.Revision, "fields": []string{"newapi_internal_base_url"}, "source": "environment"})
			after, _ := json.Marshal(map[string]any{"revision": record.Revision + 1, "fields": []string{"newapi_internal_base_url"}, "source": "database"})
			if err := (&auditRepository{db: tx}).Append(ctx, ports.AuditLog{ActorType: "system", ActorID: "worker-bootstrap", Action: "runtime_settings.bootstrap", ResourceType: "runtime_settings", ResourceID: strconv.FormatUint(record.Revision+1, 10), Reason: "Freeze the existing benefit receiver before starting settlement", BeforeJSON: before, AfterJSON: after, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		return tx.Model(&runtimeConfigModel{}).Where("id = ?", 1).Updates(updates).Error
	})
}

func lockRuntimeConfig(tx *gorm.DB) (runtimeConfigModel, error) {
	var row runtimeConfigModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).Take(&row).Error; err != nil {
		return row, fmt.Errorf("lock runtime settings (migration 00012 is required): %w", err)
	}
	return row, nil
}

func (s *runtimeConfigStore) Read(ctx context.Context, role string) (runtimeconfig.Record, error) {
	if role != runtimeconfig.RoleAPI && role != runtimeconfig.RoleWorker {
		return runtimeconfig.Record{}, runtimeconfig.ErrInvalid
	}
	var result runtimeconfig.Record
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = readRuntimeRecord(tx, role)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func readRuntimeRecord(tx *gorm.DB, role string) (runtimeconfig.Record, error) {
	var row runtimeConfigModel
	if err := tx.Where("id = ?", 1).Take(&row).Error; err != nil {
		return runtimeconfig.Record{}, fmt.Errorf("read runtime settings: %w", err)
	}
	result := runtimeconfig.Record{Revision: row.Revision, Secrets: map[string]runtimeconfig.Secret{}, Roles: map[string]runtimeconfig.Registration{}}
	if err := tx.Raw("SELECT EXISTS(SELECT 1 FROM pulse_reward_grant LIMIT 1)").Scan(&result.HasRewards).Error; err != nil {
		return result, fmt.Errorf("read runtime receiver boundary: %w", err)
	}
	if err := strictRuntimeJSON(row.ConfigJSON, &result.Config); err != nil {
		return result, fmt.Errorf("%w: invalid stored runtime configuration", runtimeconfig.ErrInvalid)
	}
	var roles []runtimeRoleModel
	if err := tx.Find(&roles).Error; err != nil {
		return result, fmt.Errorf("read runtime roles: %w", err)
	}
	for _, entry := range roles {
		registration := runtimeconfig.Registration{Role: entry.Role, PublicKey: entry.PublicKey}
		if (entry.Role != runtimeconfig.RoleAPI && entry.Role != runtimeconfig.RoleWorker) || len(entry.PublicKey) != 32 {
			return result, runtimeconfig.ErrInvalid
		}
		if err := strictRuntimeJSON(entry.EnvironmentJSON, &registration.EnvironmentFingerprints); err != nil {
			return result, runtimeconfig.ErrInvalid
		}
		result.Roles[entry.Role] = registration
	}
	var secrets []runtimeSecretModel
	// Filtering in SQL keeps ciphertext for a different process role out of
	// the driver's buffers and application memory entirely.
	if err := tx.Model(&runtimeSecretModel{}).Select("secret_name, role, CASE WHEN role = ? THEN ciphertext ELSE NULL END AS ciphertext, fingerprint", role).Find(&secrets).Error; err != nil {
		return result, fmt.Errorf("read runtime secret metadata: %w", err)
	}
	for _, entry := range secrets {
		result.Secrets[entry.Name] = runtimeconfig.Secret{Role: entry.Role, Ciphertext: entry.Ciphertext, Fingerprint: entry.Fingerprint}
	}
	return result, nil
}

func strictRuntimeJSON(payload []byte, target any) error {
	payload = bytes.TrimSpace(payload)
	if len(payload) < 2 || payload[0] != '{' {
		return runtimeconfig.ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return runtimeconfig.ErrInvalid
	}
	return nil
}

func (s *runtimeConfigStore) Update(ctx context.Context, request runtimeconfig.WriteRequest, apply func(runtimeconfig.Record) (runtimeconfig.Mutation, error)) (runtimeconfig.View, error) {
	if !validMySQLText(request.ActorID, 128) || !validMySQLText(request.RequestID, 128) || !validMySQLText(request.Reason, 500) || !validLowerHex(request.PayloadHash, 64) || apply == nil {
		return runtimeconfig.View{}, runtimeconfig.ErrInvalid
	}
	var result runtimeconfig.View
	requestHash := sha256.Sum256([]byte(request.RequestID))
	keyHash := hex.EncodeToString(requestHash[:])
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockRuntimeConfig(tx)
		if err != nil {
			return err
		}
		var replay runtimeMutationModel
		err = tx.Where("actor_id = ? AND request_hash = ?", request.ActorID, keyHash).Take(&replay).Error
		if err == nil {
			if replay.PayloadHash != request.PayloadHash {
				return runtimeconfig.ErrConflict
			}
			if err := strictRuntimeJSON(replay.ResponseJSON, &result); err != nil {
				return runtimeconfig.ErrInvalid
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("read runtime settings idempotency: %w", err)
		}
		if row.Revision != request.Revision || row.Revision >= uint64(runtimeconfig.MaxSafeInteger) {
			return runtimeconfig.ErrConflict
		}
		record, err := readRuntimeRecord(tx, runtimeconfig.RoleAPI)
		if err != nil {
			return err
		}
		change, err := apply(record)
		if err != nil {
			return err
		}
		if change.After.Revision != row.Revision+1 || change.Before.Revision != row.Revision || len(change.ChangedFields) == 0 {
			return runtimeconfig.ErrInvalid
		}
		configJSON, err := json.Marshal(change.Config)
		if err != nil {
			return runtimeconfig.ErrInvalid
		}
		now := time.Now().UTC()
		for name, secret := range change.Secrets {
			if runtimeconfig.SecretRole(name) != secret.Role || (secret.Fingerprint != "" && (!validLowerHex(secret.Fingerprint, 64) || len(secret.Ciphertext) < 61)) || (secret.Fingerprint == "" && len(secret.Ciphertext) != 0) {
				return runtimeconfig.ErrInvalid
			}
			entry := runtimeSecretModel{Name: name, Role: secret.Role, Ciphertext: secret.Ciphertext, Fingerprint: secret.Fingerprint, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&entry).Error; err != nil {
				return fmt.Errorf("write encrypted runtime secret: %w", err)
			}
		}
		updated := tx.Model(&runtimeConfigModel{}).Where("id = ? AND revision = ?", 1, row.Revision).Updates(map[string]any{"revision": row.Revision + 1, "config_json": configJSON, "updated_at": now})
		if updated.Error != nil {
			return fmt.Errorf("write runtime configuration: %w", updated.Error)
		}
		if updated.RowsAffected != 1 {
			return runtimeconfig.ErrConflict
		}
		// Audit values are restricted to field names and configured/source
		// status. Never persist a submitted secret or its fingerprint here.
		before, err := runtimeAuditJSON(change.Before, change.ChangedFields)
		if err != nil {
			return err
		}
		after, err := runtimeAuditJSON(change.After, change.ChangedFields)
		if err != nil {
			return err
		}
		audit := ports.AuditLog{ActorType: "answer_admin", ActorID: request.ActorID, Action: "runtime_settings.update", ResourceType: "runtime_settings", ResourceID: strconv.FormatUint(row.Revision+1, 10), Reason: request.Reason, BeforeJSON: before, AfterJSON: after, RequestID: request.RequestID, CreatedAt: now}
		if err := (&auditRepository{db: tx}).Append(ctx, audit); err != nil {
			return err
		}
		response, err := json.Marshal(change.After)
		if err != nil {
			return runtimeconfig.ErrInvalid
		}
		idempotency := runtimeMutationModel{ActorID: request.ActorID, RequestHash: keyHash, PayloadHash: request.PayloadHash, ResponseJSON: response, CreatedAt: now}
		if err := tx.Create(&idempotency).Error; err != nil {
			return fmt.Errorf("save runtime settings response: %w", err)
		}
		result = change.After
		return nil
	})
	return result, err
}

func runtimeAuditJSON(view runtimeconfig.View, fields []string) ([]byte, error) {
	states := map[string]runtimeconfig.SecretStatus{}
	for _, field := range fields {
		if state, exists := view.Secrets[field]; exists {
			states[field] = state
		}
	}
	return json.Marshal(struct {
		Revision uint64                                `json:"revision"`
		Fields   []string                              `json:"fields"`
		Secrets  map[string]runtimeconfig.SecretStatus `json:"secrets"`
	}{view.Revision, fields, states})
}
