package redis

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// NonceStore provides the cross-instance atomic nonce claim required by signed
// BFF and internal service requests. Redis is only used for this short-lived
// replay guard; no business fact is stored here.
type NonceStore struct {
	client *Client
	prefix string
}

func (c *Client) NewNonceStore(prefix string) (*NonceStore, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("redis is not initialized")
	}
	if prefix == "" {
		return nil, errors.New("nonce prefix is empty")
	}
	return &NonceStore{client: c, prefix: prefix}, nil
}

func (s *NonceStore) Claim(ctx context.Context, key string, expiresAt time.Time) (bool, error) {
	if s == nil || s.client == nil || s.client.client == nil {
		return false, errors.New("redis nonce store is not initialized")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return false, fmt.Errorf("nonce expiry is in the past")
	}
	return s.client.client.SetNX(ctx, s.prefix+":"+key, "1", ttl).Result()
}
