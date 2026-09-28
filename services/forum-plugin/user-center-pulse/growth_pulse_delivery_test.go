package pulse_user_center

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func waitPulseDeliveryIdle(t *testing.T, delivery *pulseExperienceDelivery) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		delivery.mu.Lock()
		idle := delivery.workers == 0 && len(delivery.pending) == 0
		delivery.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("delivery workers did not drain")
}

func TestCommunityBFFReturnsBeforeExperienceDeliveryAndSurvivesDisconnect(t *testing.T) {
	for _, operation := range []string{"actions", "rewards"} {
		t.Run(operation, func(t *testing.T) {
			router, uc, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
				result := `{"grant_id":"pg_1","period_id":9,"action_id":"action_1","reward_type":"community_exp","amount":100,"status":"pending"}`
				if operation == "rewards" {
					result = `{"rewards":[` + result + `]}`
				}
				w.Write([]byte(result))
			})
			started, release := make(chan struct{}), make(chan struct{})
			defer func() { close(release); waitPulseDeliveryIdle(t, uc.pulseDelivery) }()
			uc.pulseDelivery.sync = func(ctx context.Context, user string) error {
				if user != "7" {
					t.Errorf("expected authenticated forum identity, got %q", user)
				}
				close(started)
				<-release
				if ctx.Err() != nil {
					t.Error("browser disconnect cancelled background delivery")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("delivery must have a bounded timeout")
				}
				return nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			method, body := http.MethodGet, ""
			if operation == "actions" {
				method, body = http.MethodPost, `{"action_id":"action_1"}`
			}
			w := httptest.NewRecorder()
			returned := make(chan struct{})
			go func() {
				router.ServeHTTP(w, communityTestRequest(method, operation, body).WithContext(ctx))
				close(returned)
			}()
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("HTTP response waited for reward delivery")
			}
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"pending"`) {
				t.Fatalf("committed result missing: %d %s", w.Code, w.Body.String())
			}
			cancel()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("background delivery did not start")
			}
		})
	}
}

func TestPulseExperienceDeliveryCoalescesButKeepsWakeupDuringFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	d := &pulseExperienceDelivery{sync: func(context.Context, string) error {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return nil
	}}
	d.enqueue("7")
	<-started
	for i := 0; i < 100; i++ {
		d.enqueue("7")
	}
	close(release)
	waitPulseDeliveryIdle(t, d)
	if calls.Load() != 2 {
		t.Fatalf("expected one coalesced followup, got %d", calls.Load())
	}
}

func TestPulseExperienceDeliveryBoundsWorkersAndQueue(t *testing.T) {
	release := make(chan struct{})
	var active, peak atomic.Int32
	d := &pulseExperienceDelivery{sync: func(context.Context, string) error {
		n := active.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		<-release
		active.Add(-1)
		return nil
	}}
	for i := 0; i < 1000; i++ {
		d.enqueue(fmt.Sprint(i))
	}
	d.mu.Lock()
	count := len(d.pending)
	d.mu.Unlock()
	close(release)
	waitPulseDeliveryIdle(t, d)
	if count != 64 || peak.Load() > 2 {
		t.Fatalf("unbounded delivery: %d pending, %d workers", count, peak.Load())
	}
}

func TestPulseExperienceDeliveryRetriesWithoutAnotherBrowserRequest(t *testing.T) {
	var calls atomic.Int32
	d := &pulseExperienceDelivery{sync: func(context.Context, string) error {
		if calls.Add(1) < 3 {
			return errors.New("temporary ACK failure")
		}
		return nil
	}}
	d.enqueue("7")
	waitPulseDeliveryIdle(t, d)
	if calls.Load() != 3 {
		t.Fatalf("expected automatic retries, got %d", calls.Load())
	}
}
