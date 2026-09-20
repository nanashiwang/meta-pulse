package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
)

func TestMySQLAdminPeriodConcurrentReplayAndOverlap(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewPeriodCreateService(unit, time.Now)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	request := validAdminPeriod()
	request.Key = fmt.Sprintf("web-test-%d", time.Now().UnixNano())
	request.StartsAt = time.Now().AddDate(80, 0, 0).UTC().Truncate(time.Second)
	// This test uses an isolated database; reruns remain adjacent.
	var last *time.Time
	if err := db.QueryRowContext(ctx, "SELECT MAX(ends_at) FROM pulse_period").Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last != nil && !last.Before(request.StartsAt) {
		request.StartsAt = last.Add(time.Hour)
	}
	var group sync.WaitGroup
	failures := make([]error, 100)
	results := make([]PeriodCreateResult, 100)
	for i := range failures {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			results[i], failures[i] = svc.CreateFromAdmin(ctx, request, "42", request.Key)
		}(i)
	}
	group.Wait()
	for i, err := range failures {
		if err != nil || results[i].PeriodID != results[0].PeriodID {
			t.Fatalf("replay %d: %v", i, err)
		}
	}
	var periods, audits int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_period WHERE period_key=?", request.Key).Scan(&periods)
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pulse_audit_log WHERE request_id=?", request.Key).Scan(&audits)
	if periods != 1 || audits != 1 {
		t.Fatalf("duplicate writes %d/%d", periods, audits)
	}
	request.MultiplierBps++
	if _, err := svc.CreateFromAdmin(ctx, request, "42", request.Key); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("changed replay: %v", err)
	}
	// Different request keys and period IDs must not create overlapping windows.
	request.StartsAt = request.StartsAt.Add(PeriodLength)
	pair := make([]error, 2)
	for i := range pair {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			r := request
			r.Key = fmt.Sprintf("%s-%d", request.Key, i)
			_, pair[i] = svc.CreateFromAdmin(ctx, r, "42", r.Key)
		}(i)
	}
	group.Wait()
	success, conflicts := 0, 0
	for _, err := range pair {
		if err == nil {
			success++
		} else if errors.Is(err, ports.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("overlap race %v", pair)
	}
	// A fresh reader, like a browser refresh, sees the saved rates.
	list, err := svc.ListForAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range list {
		if p.Key == request.Key {
			found = p.TicketThresholdMilli == 1500250 && p.Rules[0].MultiplierBps == 12500
		}
	}
	if !found {
		t.Fatal("saved economics absent after reload")
	}
}
