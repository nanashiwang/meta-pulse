package pulse_user_center

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
)

func TestRedisNonceStoreIsSharedAndAtomic(t *testing.T) {
	server := miniredis.RunT(t)
	rawURL := "redis://" + server.Addr() + "/0"
	first, err := NewRedisNonceStore(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewRedisNonceStore(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	stores := []*RedisNonceStore{first, second}
	const attempts = 100
	results := make(chan bool, attempts)
	errors := make(chan error, attempts)
	var wait sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			claimed, err := stores[index%len(stores)].Claim(context.Background(), "shared-nonce", time.Now().Add(ticketTTL))
			if err != nil {
				errors <- err
				return
			}
			results <- claimed
		}(i)
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	accepted := 0
	for claimed := range results {
		if claimed {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d requests claimed one shared nonce, want 1", accepted)
	}
}

func TestRedisNonceStoreFailsClosedWhenUnavailable(t *testing.T) {
	store, err := NewRedisNonceStore("redis://127.0.0.1:1/0")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if claimed, err := store.Claim(ctx, "nonce", time.Now().Add(ticketTTL)); err == nil || claimed {
		t.Fatalf("claimed=%v err=%v", claimed, err)
	}
}

func TestConfigReceiverInstallsSharedNonceStore(t *testing.T) {
	server := miniredis.RunT(t)
	uc := &UserCenter{Config: &Config{}}
	payload := []byte(fmt.Sprintf(`{
		"newapi_base_url":"https://api.example.test",
		"pulse_base_url":"https://pulse.example.test",
		"sso_hmac_secret":"sso-secret",
		"pulse_hmac_secret":"pulse-secret",
		"nonce_redis_url":"redis://%s/0",
		"level_badge_enabled":true
	}`, server.Addr()))
	if err := uc.ConfigReceiver(payload); err != nil {
		t.Fatal(err)
	}
	store, ok := uc.Nonces.(*RedisNonceStore)
	if !ok || store == nil {
		t.Fatalf("nonce store=%T, want RedisNonceStore", uc.Nonces)
	}
	t.Cleanup(func() { _ = store.Close() })
}
