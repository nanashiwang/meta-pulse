package service

import (
	"context"
	"fmt"
	mysqlstore "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"sync"
	"testing"
	"time"
)

func TestMySQLCheckinFlashConcurrentReplayCreatesOneGrantAndOutbox(t *testing.T) {
	database, db := openMySQLIntegration(t)
	unit, err := mysqlstore.NewUnitOfWork(database)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCommunityTicketService(unit)
	s.QuotaPerUnit = 500000
	s.CheckinFlashEnabled = true
	s.now = func() time.Time { return time.Date(2095, 11, 18, 8, 0, 0, 0, time.UTC) }
	user := uint64(time.Now().UnixNano()%1000000000 + 5000000000)
	task := CheckinFlash{Day: "2095-11-18", Cents: 30, EventID: user}
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.GrantCheckinFlash(ctx, user, task); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count int
	var amount int64
	if err = db.QueryRow("SELECT COUNT(*),COALESCE(SUM(amount),0) FROM pulse_reward_grant WHERE user_id=? AND trigger_type='checkin'", user).Scan(&count, &amount); err != nil || count != 1 || amount != 150000 {
		t.Fatal(count, amount, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM pulse_settlement_outbox o JOIN pulse_reward_grant g ON g.id=o.reward_grant_id WHERE g.user_id=? AND g.trigger_type='checkin'", user).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM pulse_ledger_entry WHERE user_id=? AND asset_type='ticket'", user).Scan(&count); err != nil || count != 0 {
		t.Fatal("check-in spent a ticket", count, err)
	}
	first, err := s.GrantCheckinFlash(ctx, user, task)
	if err != nil {
		t.Fatal(err)
	}
	s.QuotaPerUnit = 100000
	replay, err := s.GrantCheckinFlash(ctx, user, task)
	if err != nil || first != replay {
		t.Fatal(first, replay, err)
	}
	task.Cents = 31
	if _, err = s.GrantCheckinFlash(ctx, user, task); err == nil {
		t.Fatal("different cents accepted")
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM pulse_reward_definition WHERE period_id=(SELECT id FROM pulse_period WHERE period_key=?)", fmt.Sprintf("%s%d", checkinFlashPrefix, 500000)).Scan(&count); err != nil || count != 150 {
		t.Fatal(count, err)
	}
}
