// forum-config validates or explicitly updates only Answer's database connection.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"
)

func field(n *yaml.Node, key string) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
func connection(root *yaml.Node) (*yaml.Node, *yaml.Node, error) {
	data := field(root, "data")
	if data == nil {
		return nil, nil, errors.New("Answer data configuration missing")
	}
	db := field(data, "database")
	if db == nil {
		return nil, nil, errors.New("Answer database configuration missing")
	}
	driver, conn := field(db, "driver"), field(db, "connection")
	if driver == nil || conn == nil || conn.Kind != yaml.ScalarNode {
		return nil, nil, errors.New("invalid Answer database configuration")
	}
	return driver, conn, nil
}
func sameTarget(a, b *mysql.Config) bool {
	return a.Net == b.Net && strings.EqualFold(a.Addr, b.Addr) && a.DBName == b.DBName && a.User == b.User
}
func execute(mode, path, dsn string, external bool) error {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) && !external && mode == "check" {
		fmt.Println("Answer not initialized; local setup remains available")
		return nil
	}
	if err != nil {
		return errors.New("Answer configuration is not readable")
	}
	var root yaml.Node
	if yaml.Unmarshal(raw, &root) != nil {
		return errors.New("invalid Answer YAML")
	}
	driver, conn, err := connection(&root)
	if err != nil {
		return err
	}
	if driver.Value != "mysql" && mode == "check" {
		if external {
			return errors.New("external Forum requires initialized MySQL configuration")
		}
		fmt.Println("Answer local initialization pending")
		return nil
	}
	target, err := mysql.ParseDSN(dsn)
	if err != nil || target.DBName == "" || target.Net != "tcp" {
		return errors.New("invalid FORUM_BINDING_GUARD_DSN")
	}
	if mode == "sync" {
		if err := pingDatabase(target); err != nil {
			return err
		}
		if driver.Value != "mysql" {
			return errors.New("initialize Answer with MySQL before synchronizing")
		}
		if conn.Value == dsn {
			fmt.Println("Answer database connection already synchronized")
			return nil
		}
		// Keep the old file intact if validation or an atomic replacement fails.
		conn.Value = dsn
		var out bytes.Buffer
		enc := yaml.NewEncoder(&out)
		enc.SetIndent(2)
		if err = enc.Encode(&root); err != nil {
			return errors.New("encode Answer configuration failed")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("invalid config file")
		}
		backup, err := os.OpenFile(path+".before-infra", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("config backup unavailable; preserve or move existing .before-infra before a new migration")
		}
		_, writeErr := backup.Write(raw)
		syncErr := backup.Sync()
		closeBackupErr := backup.Close()
		if writeErr != nil || syncErr != nil || closeBackupErr != nil {
			return errors.New("config backup failed; original configuration unchanged")
		}
		f, err := os.CreateTemp(filepath.Dir(path), ".forum-config-*")
		if err != nil {
			return err
		}
		name := f.Name()
		defer os.Remove(name)
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(out.Bytes())
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return errors.New("config write failed")
		}
		if err = os.Rename(name, path); err != nil {
			return errors.New("config replacement failed")
		}
		fmt.Println("Answer database connection updated; original saved alongside configuration")
		return nil
	}
	existing, err := mysql.ParseDSN(conn.Value)
	if err != nil || !sameTarget(existing, target) {
		return errors.New("Answer and Forum plugin database targets differ; explicit sync required")
	}
	for _, cfg := range []*mysql.Config{existing, target} {
		if err := pingDatabase(cfg); err != nil {
			return err
		}
	}
	fmt.Println("Answer and Forum plugin database preflight passed")
	return nil
}

var pingDatabase = func(cfg *mysql.Config) error {
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	if cfg.TLSConfig == "skip-verify" || cfg.TLSConfig == "preferred" {
		return errors.New("Forum MySQL TLS must verify the server")
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return errors.New("Forum database configuration failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if db.PingContext(ctx) != nil {
		return errors.New("Forum database connection failed")
	}
	return nil
}

func main() {
	mode := "check"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode != "check" && !(mode == "sync" && len(os.Args) == 3 && os.Args[2] == "--apply") {
		fmt.Fprintln(os.Stderr, "usage: forum-config check | sync --apply")
		os.Exit(2)
	}
	path := os.Getenv("FORUM_CONFIG_FILE")
	if path == "" {
		path = "/data/conf/conf/config.yaml"
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = "/data/conf/config.yaml"
		}
	}
	if err := execute(mode, path, os.Getenv("FORUM_BINDING_GUARD_DSN"), os.Getenv("FORUM_DB_MODE") == "external"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
