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

const (
	forumNonceRedisPrefix = "meta-pulse:forum:sso:nonce:"
	forumFlowRedisPrefix  = "meta-pulse:forum:sso:flow:"
)

// RedisNonceStore makes Login Tickets single-use across all Answer instances.
type RedisNonceStore struct {
	client      *redis.Client
	noncePrefix string
	flowPrefix  string
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
	return &RedisNonceStore{client: redis.NewClient(options), noncePrefix: forumNonceRedisPrefix, flowPrefix: forumFlowRedisPrefix}, nil
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
	key := s.noncePrefix + hex.EncodeToString(digest[:])
	return s.client.SetNX(ctx, key, "1", ttl).Result()
}

// Begin records that this browser deliberately started a new-api connector
// flow. The opaque value is stored hashed and sent only as an HttpOnly cookie.
func (s *RedisNonceStore) Begin(ctx context.Context, flowID string, expiresAt time.Time) error {
	if s == nil || s.client == nil || ctx == nil {
		return fmt.Errorf("forum login flow store not configured")
	}
	if flowID == "" {
		return fmt.Errorf("empty forum login flow id")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return fmt.Errorf("forum login flow already expired")
	}
	key := s.flowPrefix + hashLoginValue(flowID)
	created, err := s.client.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return err
	}
	if !created {
		return fmt.Errorf("forum login flow id collision")
	}
	return nil
}

var consumeLoginFlowScript = redis.NewScript(`
local flow = KEYS[1]
local nonce = KEYS[2]
local ttl = tonumber(ARGV[1])
if ttl == nil or ttl <= 0 then
  return 0
end
if redis.call("EXISTS", flow) ~= 1 then
  return 0
end
if redis.call("EXISTS", nonce) == 1 then
  return 0
end
redis.call("DEL", flow)
redis.call("SET", nonce, "1", "PX", ttl, "NX")
return 1
`)

// Consume atomically requires a pending browser flow and marks the signed
// ticket nonce as spent. Combining both operations prevents two callbacks from
// winning on different Answer instances.
func (s *RedisNonceStore) Consume(ctx context.Context, flowID, nonce string, expiresAt time.Time) (bool, error) {
	if s == nil || s.client == nil || ctx == nil {
		return false, fmt.Errorf("forum login flow store not configured")
	}
	if flowID == "" || nonce == "" {
		return false, nil
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return false, nil
	}
	result, err := consumeLoginFlowScript.Run(ctx, s.client, []string{
		s.flowPrefix + hashLoginValue(flowID),
		s.noncePrefix + hashLoginValue(nonce),
	}, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func hashLoginValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (s *RedisNonceStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}
