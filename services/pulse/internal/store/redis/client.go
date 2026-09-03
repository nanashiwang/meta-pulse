// Package redis owns runtime coordination only. It must never be used as a
// ledger, budget, reward, or settlement source of truth.
package redis

import (
	"context"
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
