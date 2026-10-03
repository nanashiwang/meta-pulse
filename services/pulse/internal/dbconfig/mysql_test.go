package dbconfig

import (
	mysql "github.com/go-sql-driver/mysql"
	"strings"
	"testing"
	"time"
)

func TestDefaultsAndExplicitTimeouts(t *testing.T) {
	raw, err := Normalize("u:secret@tcp(db:3306)/pulse?timeout=9s&tls=true")
	if err != nil {
		t.Fatal(err)
	}
	c, err := mysql.ParseDSN(raw)
	if err != nil || c.Timeout != 9*time.Second || c.ReadTimeout != 30*time.Second || c.TLSConfig != "true" {
		t.Fatal("connection defaults lost")
	}
	for _, dsn := range []string{"u:secret@tcp(db:3306)/pulse?tls=skip-verify", "u:secret@tcp(db:3306)/pulse?tls=preferred", "malformed:secret"} {
		_, err = Normalize(dsn)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsafe DSN accepted or secret exposed")
		}
	}
}
