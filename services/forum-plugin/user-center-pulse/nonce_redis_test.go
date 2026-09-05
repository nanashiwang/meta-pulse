package pulse_user_center

import (
	"context"
	"fmt"
	"strings"
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

func TestRedisLoginFlowAndTicketNonceAreConsumedAtomically(t *testing.T) {
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

	expiresAt := time.Now().Add(ticketTTL)
	if err := first.Begin(context.Background(), "browser-flow", time.Now().Add(loginFlowTTL)); err != nil {
		t.Fatal(err)
	}
	const attempts = 100
	results := make(chan bool, attempts)
	var wait sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			store := first
			if index%2 == 1 {
				store = second
			}
			accepted, consumeErr := store.Consume(context.Background(), "browser-flow", "signed-ticket", expiresAt)
			if consumeErr != nil {
				t.Errorf("consume: %v", consumeErr)
				return
			}
			results <- accepted
		}(i)
	}
	wait.Wait()
	close(results)
	accepted := 0
	for result := range results {
		if result {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted=%d, want 1", accepted)
	}
	if ok, err := first.Consume(context.Background(), "missing-flow", "new-ticket", expiresAt); err != nil || ok {
		t.Fatalf("unsolicited callback accepted=%v err=%v", ok, err)
	}
}

func TestConfigReceiverInstallsSharedNonceStore(t *testing.T) {
	server := miniredis.RunT(t)
	guard := &fakeBindingGuard{}
	uc := &UserCenter{Config: &Config{}, newBindingGuard: func(string) (BindingGuard, error) { return guard, nil }}
	t.Setenv("FORUM_BINDING_GUARD_DSN", "ignored-by-test")
	payload := []byte(fmt.Sprintf(`{
		"newapi_base_url":"https://api.example.test",
		"pulse_base_url":"https://pulse.example.test",
		"sso_hmac_secret":"%s",
		"pulse_hmac_secret":"%s",
		"nonce_redis_url":"redis://%s/0",
		"level_badge_enabled":true
	}`, strings.Repeat("s", minimumConfigSecretLength), strings.Repeat("p", minimumConfigSecretLength), server.Addr()))
	if err := uc.ConfigReceiver(payload); err != nil {
		t.Fatal(err)
	}
	store, ok := uc.Logins.(*RedisNonceStore)
	if !ok || store == nil {
		t.Fatalf("login store=%T, want RedisNonceStore", uc.Logins)
	}
	if uc.Guard != guard {
		t.Fatalf("binding guard=%T, want configured fake", uc.Guard)
	}
	t.Cleanup(func() { _ = store.Close() })
}

func TestConfigReceiverRejectsNilBindingGuard(t *testing.T) {
	server := miniredis.RunT(t)
	oldConfig := &Config{NewAPIBaseURL: "https://old.example.test"}
	uc := &UserCenter{
		Config: oldConfig,
		newBindingGuard: func(string) (BindingGuard, error) {
			return nil, nil
		},
	}
	t.Setenv("FORUM_BINDING_GUARD_DSN", "ignored-by-test")
	payload := []byte(fmt.Sprintf(`{
		"newapi_base_url":"https://api.example.test",
		"pulse_base_url":"https://pulse.example.test",
		"sso_hmac_secret":"%s",
		"pulse_hmac_secret":"%s",
		"nonce_redis_url":"redis://%s/0",
		"level_badge_enabled":true
	}`, strings.Repeat("s", minimumConfigSecretLength), strings.Repeat("p", minimumConfigSecretLength), server.Addr()))
	if err := uc.ConfigReceiver(payload); err == nil {
		t.Fatal("nil binding guard was accepted")
	}
	if uc.Config != oldConfig || uc.Logins != nil || uc.Guard != nil {
		t.Fatal("failed config replaced the last known-good runtime")
	}
}
