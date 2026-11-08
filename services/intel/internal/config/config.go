// Package config reads Intel's environment. Intel has no secrets beyond its
// own database DSN: the public API is anonymous and read-only.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Environment   string
	HTTPAddr      string
	DBDSN         string
	ScoreInterval time.Duration
	SpeedWindow   time.Duration
	CacheTTL      time.Duration
	// AdminSecrets holds the active and optional previous HMAC secret shared
	// with the forum plugin. Empty disables the admin API entirely.
	AdminSecrets [][]byte
}

func Load() (Config, error) {
	c := Config{
		Environment: env("INTEL_ENV", "development"),
		HTTPAddr:    env("INTEL_HTTP_ADDR", ":8090"),
		DBDSN:       os.Getenv("INTEL_DB_DSN"),
	}
	var err error
	if c.ScoreInterval, err = duration("INTEL_SCORE_INTERVAL", 5*time.Minute, time.Minute, 24*time.Hour); err != nil {
		return c, err
	}
	if c.SpeedWindow, err = duration("INTEL_SPEED_WINDOW", 7*24*time.Hour, time.Hour, 90*24*time.Hour); err != nil {
		return c, err
	}
	if c.CacheTTL, err = duration("INTEL_CACHE_TTL", 30*time.Second, time.Second, 10*time.Minute); err != nil {
		return c, err
	}
	for _, key := range []string{"INTEL_ADMIN_HMAC_SECRET", "INTEL_ADMIN_HMAC_SECRET_PREVIOUS"} {
		v := os.Getenv(key)
		if v == "" {
			continue
		}
		if len(v) < 32 {
			return c, fmt.Errorf("%s must be at least 32 characters", key)
		}
		c.AdminSecrets = append(c.AdminSecrets, []byte(v))
	}
	if os.Getenv("INTEL_ADMIN_HMAC_SECRET") == "" && len(c.AdminSecrets) > 0 {
		return c, errors.New("INTEL_ADMIN_HMAC_SECRET_PREVIOUS requires INTEL_ADMIN_HMAC_SECRET")
	}
	if c.DBDSN == "" {
		return c, errors.New("INTEL_DB_DSN is required")
	}
	return c, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def, min, max time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < min || d > max {
		return 0, fmt.Errorf("%s must be a duration between %s and %s", key, min, max)
	}
	return d, nil
}
