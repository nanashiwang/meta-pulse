package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

func TestSyncPreservesSettingsAndRequiresMatchingTargets(t *testing.T) {
	oldPing := pingDatabase
	pingDatabase = func(*mysql.Config) error { return nil }
	defer func() { pingDatabase = oldPing }()
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "data:\n  database:\n    driver: mysql\n    connection: old:p@tcp(old:3306)/forum\nservice_config:\n  upload_path: /data/custom/uploads\nserver:\n  http:\n    addr: :80\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	target := "new:secret@tcp(new:3306)/forum?tls=true"
	if err := execute("check", path, target, true); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("mismatch allowed or secret exposed")
	}
	if err := execute("sync", path, target, true); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var doc yaml.Node
	if yaml.Unmarshal(raw, &doc) != nil {
		t.Fatal("invalid YAML")
	}
	_, conn, err := connection(&doc)
	if err != nil || conn.Value != target {
		t.Fatal("connection not updated")
	}
	if !strings.Contains(string(raw), "/data/custom/uploads") {
		t.Fatal("unrelated setting lost")
	}
	saved, _ := os.ReadFile(path + ".before-infra")
	if string(saved) != original {
		t.Fatal("original config not saved")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions")
	}
}
func TestTargetComparisonAndMissingExternalConfig(t *testing.T) {
	a, _ := mysql.ParseDSN("u:p@tcp(db:3306)/a")
	b, _ := mysql.ParseDSN("u:other@tcp(db:3306)/a")
	if !sameTarget(a, b) {
		t.Fatal("password differences are checked by real connection")
	}
	b.DBName = "b"
	if sameTarget(a, b) {
		t.Fatal("different databases accepted")
	}
	if execute("check", filepath.Join(t.TempDir(), "missing"), "u:p@tcp(db:3306)/a", true) == nil {
		t.Fatal("external missing config accepted")
	}
}

func TestSyncFailurePreservesOriginalAndPriorBackup(t *testing.T) {
	oldPing := pingDatabase
	defer func() { pingDatabase = oldPing }()
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "data:\n  database:\n    driver: mysql\n    connection: old:p@tcp(old:3306)/forum\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	pingDatabase = func(*mysql.Config) error { return errors.New("unreachable") }
	if execute("sync", path, "new:p@tcp(new:3306)/forum", true) == nil {
		t.Fatal("unreachable target accepted")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != original {
		t.Fatal("failed preflight changed config")
	}
	if _, err := os.Stat(path + ".before-infra"); !os.IsNotExist(err) {
		t.Fatal("failed preflight created backup")
	}
	pingDatabase = func(*mysql.Config) error { return nil }
	if err := os.WriteFile(path+".before-infra", []byte("prior migration"), 0600); err != nil {
		t.Fatal(err)
	}
	if execute("sync", path, "new:p@tcp(new:3306)/forum", true) == nil {
		t.Fatal("prior backup overwritten")
	}
	raw, _ = os.ReadFile(path + ".before-infra")
	if string(raw) != "prior migration" {
		t.Fatal("prior backup changed")
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != original {
		t.Fatal("backup failure changed config")
	}
}
