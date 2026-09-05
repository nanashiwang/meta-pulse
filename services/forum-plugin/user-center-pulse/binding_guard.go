package pulse_user_center

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

const (
	pluginSlug = "pulse_user_center"

	bindingGuardLockName       = "meta_pulse_forum_binding_guard_v1"
	bindingGuardExternalColumn = "meta_pulse_external_id_guard"
	bindingGuardUserColumn     = "meta_pulse_user_id_guard"
	bindingGuardExternalIndex  = "ux_meta_pulse_external_id"
	bindingGuardUserIndex      = "ux_meta_pulse_user_id"
	bindingGuardInsertTrigger  = "trg_meta_pulse_binding_insert"
	bindingGuardUpdateTrigger  = "trg_meta_pulse_binding_update"
	bindingGuardDeleteTrigger  = "trg_meta_pulse_binding_delete"
)

// BindingGuard protects Answer's external-login table from duplicate,
// concurrent or silent Meta Pulse account transfers. The table remains
// Answer's source of truth; this guard only adds database invariants that
// Answer v1.7.1 itself does not provide.
type BindingGuard interface {
	Ensure(context.Context) error
	Ready(context.Context) error
	Close() error
}

type mysqlBindingGuard struct{ db *sql.DB }

func NewMySQLBindingGuard(rawDSN string) (BindingGuard, error) {
	rawDSN = strings.TrimSpace(rawDSN)
	if rawDSN == "" {
		return nil, errors.New("FORUM_BINDING_GUARD_DSN is required")
	}
	cfg, err := mysqlDriver.ParseDSN(rawDSN)
	if err != nil {
		return nil, fmt.Errorf("parse forum binding guard DSN: %w", err)
	}
	if cfg.DBName == "" {
		return nil, errors.New("forum binding guard DSN must select a database")
	}
	cfg.MultiStatements = false
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open forum binding guard database: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &mysqlBindingGuard{db: db}, nil
}

func (g *mysqlBindingGuard) Close() error {
	if g == nil || g.db == nil {
		return nil
	}
	return g.db.Close()
}

func (g *mysqlBindingGuard) Ensure(ctx context.Context) error {
	if g == nil || g.db == nil || ctx == nil {
		return errors.New("forum binding guard is not configured")
	}
	conn, err := g.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect forum binding guard database: %w", err)
	}
	defer conn.Close()

	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 10)", bindingGuardLockName).Scan(&locked); err != nil {
		return fmt.Errorf("acquire forum binding guard lock: %w", err)
	}
	if locked != 1 {
		return errors.New("forum binding guard migration lock is busy")
	}
	defer func() {
		var released sql.NullInt64
		_ = conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", bindingGuardLockName).Scan(&released)
	}()

	if err := requireExternalLoginTable(ctx, conn); err != nil {
		return err
	}
	if err := rejectUnsafeExistingBindings(ctx, conn); err != nil {
		return err
	}
	if err := ensureGeneratedColumn(ctx, conn, bindingGuardExternalColumn, "VARCHAR(128)", "external_id"); err != nil {
		return err
	}
	if err := ensureGeneratedColumn(ctx, conn, bindingGuardUserColumn, "BIGINT", "user_id"); err != nil {
		return err
	}
	if err := ensureUniqueIndex(ctx, conn, bindingGuardExternalIndex, bindingGuardExternalColumn); err != nil {
		return err
	}
	if err := ensureUniqueIndex(ctx, conn, bindingGuardUserIndex, bindingGuardUserColumn); err != nil {
		return err
	}
	if err := ensureTrigger(ctx, conn, bindingGuardInsertTrigger, "BEFORE", "INSERT", insertTriggerDDL()); err != nil {
		return err
	}
	if err := ensureTrigger(ctx, conn, bindingGuardUpdateTrigger, "BEFORE", "UPDATE", updateTriggerDDL()); err != nil {
		return err
	}
	if err := ensureTrigger(ctx, conn, bindingGuardDeleteTrigger, "BEFORE", "DELETE", deleteTriggerDDL()); err != nil {
		return err
	}
	// DDL is not transactional in MySQL. Re-scan after every constraint and
	// trigger exists so a binding inserted during the migration window cannot
	// survive without validation.
	if err := rejectUnsafeExistingBindings(ctx, conn); err != nil {
		return err
	}
	return inspectBindingGuard(ctx, conn)
}

func (g *mysqlBindingGuard) Ready(ctx context.Context) error {
	if g == nil || g.db == nil || ctx == nil {
		return errors.New("forum binding guard is not configured")
	}
	conn, err := g.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("connect forum binding guard database: %w", err)
	}
	defer conn.Close()
	return inspectBindingGuard(ctx, conn)
}

type queryExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireExternalLoginTable(ctx context.Context, db queryExecer) error {
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_external_login'`).Scan(&count); err != nil {
		return fmt.Errorf("inspect Answer external-login table: %w", err)
	}
	if count != 1 {
		return errors.New("Answer user_external_login table is missing; finish Answer initialization before enabling the plugin")
	}
	return nil
}

func rejectUnsafeExistingBindings(ctx context.Context, db queryExecer) error {
	checks := []struct {
		name  string
		query string
	}{
		{"invalid", `SELECT COUNT(*) FROM user_external_login WHERE provider = ? AND (user_id <= 0 OR external_id NOT REGEXP '^[1-9][0-9]*$' OR CHAR_LENGTH(external_id) > 20 OR (CHAR_LENGTH(external_id) = 20 AND external_id > '18446744073709551615'))`},
		{"duplicate external identity", `SELECT COUNT(*) FROM (SELECT external_id FROM user_external_login WHERE provider = ? GROUP BY external_id HAVING COUNT(*) > 1) AS duplicate_bindings`},
		{"duplicate forum identity", `SELECT COUNT(*) FROM (SELECT user_id FROM user_external_login WHERE provider = ? GROUP BY user_id HAVING COUNT(*) > 1) AS duplicate_bindings`},
	}
	for _, check := range checks {
		var count int
		if err := db.QueryRowContext(ctx, check.query, pluginSlug).Scan(&count); err != nil {
			return fmt.Errorf("scan %s Meta Pulse bindings: %w", check.name, err)
		}
		if count > 0 {
			return fmt.Errorf("refusing binding guard migration: found %d %s Meta Pulse binding group(s)", count, check.name)
		}
	}
	return nil
}

func ensureGeneratedColumn(ctx context.Context, db queryExecer, name, sqlType, sourceColumn string) error {
	var dataType, columnType, extra, expression string
	err := db.QueryRowContext(ctx, `
SELECT DATA_TYPE, COLUMN_TYPE, EXTRA, GENERATION_EXPRESSION
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_external_login' AND COLUMN_NAME = ?`, name).
		Scan(&dataType, &columnType, &extra, &expression)
	if errors.Is(err, sql.ErrNoRows) {
		ddl := fmt.Sprintf("ALTER TABLE user_external_login ADD COLUMN `%s` %s GENERATED ALWAYS AS (IF(provider = '%s', %s, NULL)) STORED", name, sqlType, pluginSlug, sourceColumn)
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create forum binding guard column %s: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect forum binding guard column %s: %w", name, err)
	}
	if !validGuardColumn(dataType, columnType, extra, expression, sourceColumn) {
		return fmt.Errorf("forum binding guard column %s has an unexpected definition (data_type=%q column_type=%q extra=%q expression=%q)", name, dataType, columnType, extra, expression)
	}
	return nil
}

func validGuardColumn(dataType, columnType, extra, expression, sourceColumn string) bool {
	normalized := normalizeSQL(expression)
	expected := normalizeSQL(fmt.Sprintf("IF(provider = '%s', %s, NULL)", pluginSlug, sourceColumn))
	if normalized != expected || !strings.Contains(strings.ToLower(extra), "stored generated") {
		return false
	}
	switch sourceColumn {
	case "external_id":
		return strings.EqualFold(dataType, "varchar") && strings.EqualFold(columnType, "varchar(128)")
	case "user_id":
		return strings.EqualFold(dataType, "bigint") && strings.HasPrefix(strings.ToLower(columnType), "bigint")
	default:
		return false
	}
}

type indexPart struct {
	column    string
	nonUnique int
	sequence  int
}

func bindingGuardIndexParts(ctx context.Context, db queryExecer, indexName string) ([]indexPart, error) {
	rows, err := db.QueryContext(ctx, `
SELECT COLUMN_NAME, NON_UNIQUE, SEQ_IN_INDEX
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_external_login' AND INDEX_NAME = ?
ORDER BY SEQ_IN_INDEX`, indexName)
	if err != nil {
		return nil, fmt.Errorf("inspect forum binding guard index %s: %w", indexName, err)
	}
	defer rows.Close()
	var parts []indexPart
	for rows.Next() {
		var part indexPart
		if err := rows.Scan(&part.column, &part.nonUnique, &part.sequence); err != nil {
			return nil, fmt.Errorf("scan forum binding guard index %s: %w", indexName, err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate forum binding guard index %s: %w", indexName, err)
	}
	return parts, nil
}

func validBindingGuardIndex(parts []indexPart, columnName string) bool {
	return len(parts) == 1 && parts[0].column == columnName && parts[0].nonUnique == 0 && parts[0].sequence == 1
}

func ensureUniqueIndex(ctx context.Context, db queryExecer, indexName, columnName string) error {
	parts, err := bindingGuardIndexParts(ctx, db, indexName)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		ddl := fmt.Sprintf("CREATE UNIQUE INDEX `%s` ON user_external_login (`%s`)", indexName, columnName)
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create forum binding guard index %s: %w", indexName, err)
		}
		return nil
	}
	if !validBindingGuardIndex(parts, columnName) {
		return fmt.Errorf("forum binding guard index %s has an unexpected definition", indexName)
	}
	return nil
}

func ensureTrigger(ctx context.Context, db queryExecer, name, timing, event, ddl string) error {
	var actualTiming, actualEvent, statement string
	err := db.QueryRowContext(ctx, `
SELECT ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
FROM information_schema.TRIGGERS
WHERE TRIGGER_SCHEMA = DATABASE() AND EVENT_OBJECT_TABLE = 'user_external_login' AND TRIGGER_NAME = ?`, name).
		Scan(&actualTiming, &actualEvent, &statement)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("create forum binding guard trigger %s: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect forum binding guard trigger %s: %w", name, err)
	}
	if !strings.EqualFold(actualTiming, timing) || !strings.EqualFold(actualEvent, event) || !validGuardTrigger(event, statement) {
		return fmt.Errorf("forum binding guard trigger %s has an unexpected definition", name)
	}
	return nil
}

func inspectBindingGuard(ctx context.Context, db queryExecer) error {
	if err := requireExternalLoginTable(ctx, db); err != nil {
		return err
	}
	for _, column := range []struct{ name, source string }{{bindingGuardExternalColumn, "external_id"}, {bindingGuardUserColumn, "user_id"}} {
		var dataType, columnType, extra, expression string
		if err := db.QueryRowContext(ctx, `
SELECT DATA_TYPE, COLUMN_TYPE, EXTRA, GENERATION_EXPRESSION
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'user_external_login' AND COLUMN_NAME = ?`, column.name).
			Scan(&dataType, &columnType, &extra, &expression); err != nil {
			return fmt.Errorf("forum binding guard column %s is not ready: %w", column.name, err)
		}
		if !validGuardColumn(dataType, columnType, extra, expression, column.source) {
			return fmt.Errorf("forum binding guard column %s is not ready (data_type=%q column_type=%q extra=%q expression=%q)", column.name, dataType, columnType, extra, expression)
		}
	}
	for _, index := range []struct{ name, column string }{{bindingGuardExternalIndex, bindingGuardExternalColumn}, {bindingGuardUserIndex, bindingGuardUserColumn}} {
		parts, err := bindingGuardIndexParts(ctx, db, index.name)
		if err != nil {
			return err
		}
		if !validBindingGuardIndex(parts, index.column) {
			return fmt.Errorf("forum binding guard index %s is not ready", index.name)
		}
	}
	for _, trigger := range []struct{ name, timing, event string }{
		{bindingGuardInsertTrigger, "BEFORE", "INSERT"},
		{bindingGuardUpdateTrigger, "BEFORE", "UPDATE"},
		{bindingGuardDeleteTrigger, "BEFORE", "DELETE"},
	} {
		var timing, event, statement string
		if err := db.QueryRowContext(ctx, `
SELECT ACTION_TIMING, EVENT_MANIPULATION, ACTION_STATEMENT
FROM information_schema.TRIGGERS
WHERE TRIGGER_SCHEMA = DATABASE() AND EVENT_OBJECT_TABLE = 'user_external_login' AND TRIGGER_NAME = ?`, trigger.name).
			Scan(&timing, &event, &statement); err != nil {
			return fmt.Errorf("forum binding guard trigger %s is not ready: %w", trigger.name, err)
		}
		if !strings.EqualFold(timing, trigger.timing) || !strings.EqualFold(event, trigger.event) || !validGuardTrigger(trigger.event, statement) {
			return fmt.Errorf("forum binding guard trigger %s is not ready", trigger.name)
		}
	}
	return nil
}

func validGuardTrigger(event, statement string) bool {
	var expected string
	switch strings.ToUpper(event) {
	case "INSERT":
		expected = insertTriggerDDL()
	case "UPDATE":
		expected = updateTriggerDDL()
	case "DELETE":
		expected = deleteTriggerDDL()
	default:
		return false
	}
	return normalizeTriggerSQL(triggerActionStatement(statement)) == normalizeTriggerSQL(triggerActionStatement(expected))
}

func triggerActionStatement(ddl string) string {
	upper := strings.ToUpper(ddl)
	if index := strings.Index(upper, "BEGIN"); index >= 0 {
		return ddl[index:]
	}
	return ddl
}

var mysqlCharsetIntroducer = regexp.MustCompile(`(^|[^a-z0-9_])_[a-z0-9]+\x27`)

func normalizeSQL(value string) string {
	return strings.NewReplacer("(", "", ")", "").Replace(normalizeSQLTokens(value))
}

// Trigger parentheses are semantically significant. Generated expressions are
// normalized more loosely because MySQL adds redundant parentheses, but trigger
// validation must preserve grouping so a same-named trigger cannot weaken an
// invariant by rearranging boolean precedence.
func normalizeTriggerSQL(value string) string { return normalizeSQLTokens(value) }

func normalizeSQLTokens(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, `\'`, "'")
	value = strings.ReplaceAll(value, "`", "")
	value = mysqlCharsetIntroducer.ReplaceAllString(value, "$1'")
	return strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(value)
}

func insertTriggerDDL() string {
	return fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON user_external_login FOR EACH ROW
BEGIN
  IF NEW.provider = '%s' AND (
    NEW.user_id <= 0
    OR NEW.external_id NOT REGEXP '^[1-9][0-9]*$'
    OR CHAR_LENGTH(NEW.external_id) > 20
    OR (CHAR_LENGTH(NEW.external_id) = 20 AND NEW.external_id > '18446744073709551615')
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'invalid Meta Pulse binding';
  END IF;
END`, bindingGuardInsertTrigger, pluginSlug)
}

func updateTriggerDDL() string {
	return fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON user_external_login FOR EACH ROW
BEGIN
  IF (OLD.provider = '%s' OR NEW.provider = '%s') AND NOT (
    OLD.provider <=> NEW.provider AND OLD.external_id <=> NEW.external_id AND OLD.user_id <=> NEW.user_id AND OLD.created_at <=> NEW.created_at
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Meta Pulse binding is immutable';
  END IF;
END`, bindingGuardUpdateTrigger, pluginSlug, pluginSlug)
}

func deleteTriggerDDL() string {
	return fmt.Sprintf(`CREATE TRIGGER %s BEFORE DELETE ON user_external_login FOR EACH ROW
BEGIN
  IF OLD.provider = '%s' THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'Meta Pulse binding requires audited administrator correction';
  END IF;
END`, bindingGuardDeleteTrigger, pluginSlug)
}
