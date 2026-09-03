package pulse_user_center

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const forumNonceRedisPrefix = "meta-pulse:forum:sso:nonce:"

// RedisNonceStore makes Login Tickets single-use across all Answer instances.
type RedisNonceStore struct {
	client *redis.Client
	prefix string
}

func NewRedisNonceStore(rawURL string) (*RedisNonceStore, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("forum nonce redis url not configured")
	}
	options, err := redis.ParseURL(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("parse forum nonce redis url: %w", err)
	}
	options.DialTimeout = 3 * time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = 2 * time.Second
	return &RedisNonceStore{client: redis.NewClient(options), prefix: forumNonceRedisPrefix}, nil
}

func (s *RedisNonceStore) Ping(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("forum nonce redis store not configured")
	}
	return s.client.Ping(ctx).Err()
}

func (s *RedisNonceStore) Claim(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	if s == nil || s.client == nil || ctx == nil {
		return false, fmt.Errorf("forum nonce redis store not configured")
	}
	if nonce == "" {
		return false, fmt.Errorf("empty login ticket nonce")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return false, nil
	}
	digest := sha256.Sum256([]byte(nonce))
	key := s.prefix + hex.EncodeToString(digest[:])
	return s.client.SetNX(ctx, key, "1", ttl).Result()
}

func (s *RedisNonceStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
