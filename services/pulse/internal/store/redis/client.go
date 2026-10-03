// Package redis owns runtime coordination only. It must never be used as a
// ledger, budget, reward, or settlement source of truth.
package redis

import (
	"context"
	"crypto/tls"
	"fmt"

	redisv9 "github.com/redis/go-redis/v9"
)

type Client struct {
	client *redisv9.Client
}

func Open(addr, password string, database int) (*Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("redis address is empty")
	}
	if database < 0 {
		return nil, fmt.Errorf("redis database must be non-negative")
	}
	return &Client{client: redisv9.NewClient(&redisv9.Options{
		Addr:     addr,
		Password: password,
		DB:       database,
	})}, nil
}

func (c *Client) Raw() *redisv9.Client {
	if c == nil {
		return nil
	}
	return c.client
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("redis is not initialized")
	}
	return c.client.Ping(ctx).Err()
}

func (c *Client) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

// OpenConfigured preserves the legacy address settings; an explicit URL owns
// authentication, database and TLS together. Parse errors never echo credentials.
func OpenConfigured(rawURL, addr, password string, database int) (*Client, error) {
	if rawURL == "" {
		return Open(addr, password, database)
	}
	options, err := redisv9.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PULSE_REDIS_URL")
	}
	if options.TLSConfig != nil {
		if options.TLSConfig.InsecureSkipVerify {
			return nil, fmt.Errorf("Redis TLS must verify the server")
		}
		options.TLSConfig.MinVersion = tls.VersionTLS12
	}
	return &Client{client: redisv9.NewClient(options)}, nil
}
