package pulse_user_center

import (
	"context"
	"sync"
	"time"

	"github.com/segmentfault/pacman/log"
)

// This bounded queue only wakes delivery; Pulse's durable outbox remains the
// source of truth. No request context, browser identity or reward amount is
// retained. Every attempt rechecks the live protected binding and uses the
// existing grant/ACK idempotency protocol.
type pulseExperienceDelivery struct {
	mu      sync.Mutex
	pending map[string]bool // true when another refresh arrived during this job
	queue   []string
	workers int
	sync    func(context.Context, string) error
}

func (uc *UserCenter) schedulePulseExperience(forumID string) {
	uc.pulseDeliveryOnce.Do(func() {
		uc.pulseDelivery = &pulseExperienceDelivery{sync: uc.syncPulseExperience}
	})
	uc.pulseDelivery.enqueue(forumID)
}

func (d *pulseExperienceDelivery) enqueue(user string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		d.pending = make(map[string]bool)
	}
	if _, exists := d.pending[user]; exists {
		d.pending[user] = true
		return
	}
	if len(d.pending) >= 64 {
		// A future reward/history refresh will wake the persisted deliveries.
		return
	}
	d.pending[user] = false
	d.queue = append(d.queue, user)
	if d.workers < 2 {
		d.workers++
		go d.run()
	}
}

func (d *pulseExperienceDelivery) run() {
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.workers--
			d.mu.Unlock()
			return
		}
		user := d.queue[0]
		d.queue = d.queue[1:]
		d.pending[user] = false
		d.mu.Unlock()

		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt) * time.Second)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			err = d.sync(ctx, user)
			cancel()
			if err == nil {
				break
			}
		}
		if err != nil {
			log.Warn("community Pulse experience delivery deferred; durable outbox retained for next refresh")
		}

		d.mu.Lock()
		if d.pending[user] {
			// A new draw can commit while the preceding delivery is finishing.
			// Coalesce those wakeups, but never lose the final one.
			d.queue = append(d.queue, user)
		} else {
			delete(d.pending, user)
		}
		d.mu.Unlock()
	}
}
