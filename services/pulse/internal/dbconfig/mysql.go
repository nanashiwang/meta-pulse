// Package dbconfig contains transport defaults, never business or account rules.
package dbconfig

import (
	"errors"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

func Normalize(raw string) (string, error) {
	c, err := mysql.ParseDSN(raw)
	if err != nil || c.DBName == "" {
		return "", errors.New("invalid MySQL connection configuration")
	}
	if c.TLSConfig == "skip-verify" || c.TLSConfig == "preferred" {
		return "", errors.New("MySQL TLS must verify the server")
	}
	if c.Timeout == 0 {
		c.Timeout = 5 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}
	return c.FormatDSN(), nil
}
