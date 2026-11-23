package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/internal/ports"
	ms "github.com/nanashiwang/meta-pulse/internal/store/mysql"
	"testing"
	"time"
)

func TestMySQLGiftCommittedOrderReplay(t *testing.T) {
	testForumFlashReceipt(t, false)
}

func TestMySQLContentAwardReceiptReplay(t *testing.T) {
	testForumFlashReceipt(t, true)
}

func testForumFlashReceipt(t *testing.T, award bool) {
	t.Helper()
	db, sqlDB := openMySQLIntegration(t)
	ctx := context.Background()
	u, e := ms.NewUnitOfWork(db)
	if e != nil {
		t.Fatal(e)
	}
	s := NewCommunityTicketService(u)
	s.CheckinFlashEnabled = true
	s.QuotaPerUnit = 500200
	user := uint64(time.Now().UnixNano()%100000000 + 8000000000)
	g := ms.GiftProjection{DB: sqlDB}
	o := GiftOrder{Round: user, Forum: user, Actor: 1, Flash: 199}
	reason := "限时赠礼"
	if award {
		o.Round |= 1 << 62
		o.Flash = 5000
		reason = "优秀内容评选"
	}
	if e = g.QueueGift(ctx, user, o); e != nil {
		t.Fatal(e)
	}
	if e = g.QueueGift(ctx, user, o); e != nil {
		t.Fatal(e)
	}
	other := o
	other.Flash++
	if g.QueueGift(ctx, user, other) == nil || g.QueueGift(ctx, user+1, o) == nil {
		t.Fatal("changed or redirected instruction accepted")
	}
	v, e := s.GrantGiftFlash(ctx, user, o.Round, o.Flash)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		next, e := s.GrantGiftFlash(ctx, user, o.Round, o.Flash)
		if e != nil || next != v {
			t.Fatal(next, e)
		}
	}
	if _, e = s.GrantGiftFlash(ctx, user, o.Round, o.Flash+1); !errors.Is(e, ports.ErrConflict) {
		t.Fatal(e)
	}
	var count, amount int64
	if e = sqlDB.QueryRow("SELECT COUNT(*),COALESCE(SUM(amount),0) FROM pulse_reward_grant WHERE user_id=? AND trigger_type='gift' AND reason=?", user, reason).Scan(&count, &amount); e != nil || count != 1 || amount != o.Flash*5002 {
		t.Fatal(count, amount, e)
	}
	if _, e = sqlDB.Exec("INSERT INTO pulse_gift_delivery(round_id,user_id,grant_id) VALUES(?,?,?)", o.Round, user, v.GrantID); e != nil {
		t.Fatal(e)
	}
	paid, _, e := g.GiftPaid(ctx, user, o.Round)
	if e != nil || paid {
		t.Fatal(paid, e)
	}
	var box *ports.SettlementOutbox
	e = u.Do(ctx, func(r ports.Repositories) error {
		grant, err := r.Reward.FindGrantByPublicID(ctx, v.GrantID)
		if err != nil {
			return err
		}
		box, err = r.Settlement.FindByGrant(ctx, grant.ID)
		return err
	})
	if e != nil {
		t.Fatal(e)
	}
	testClock := time.Date(1991, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, e = sqlDB.Exec("UPDATE pulse_settlement_outbox SET next_attempt_at=? WHERE id=?", testClock, box.ID); e != nil {
		t.Fatal(e)
	}
	e = u.Do(ctx, func(r ports.Repositories) error {
		leased, err := r.Settlement.ClaimDue(ctx, testClock, 1, testClock.Add(time.Minute))
		if err != nil {
			return err
		}
		if len(leased) != 1 || leased[0].ID != box.ID {
			return fmt.Errorf("unexpected test lease")
		}
		box = &leased[0]
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	remote := &fakeBenefitClient{grantResponse: ports.BenefitGrantResponse{Applied: true, SourceRef: v.GrantID}}
	settlement, e := NewSettlementService(u, remote, SettlementConfig{BatchSize: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = settlement.processOne(ctx, *box); e != nil {
		t.Fatal(e)
	}
	paid, _, e = g.GiftPaid(ctx, user, o.Round)
	if e != nil || !paid || remote.grantCalls != 1 {
		t.Fatal("settled result not confirmed", e)
	}
	t.Cleanup(func() {
		for _, q := range []string{"DELETE o FROM pulse_settlement_outbox o JOIN pulse_reward_grant g ON o.reward_grant_id=g.id WHERE g.user_id=?", "DELETE FROM pulse_reward_grant WHERE user_id=? AND trigger_type='gift'", "DELETE FROM pulse_gift_delivery WHERE user_id=?", "DELETE FROM pulse_gift_order WHERE user_id=?"} {
			sqlDB.Exec(q, user)
		}
		key := fmt.Sprintf("%sgift-%d", checkinFlashPrefix, s.QuotaPerUnit)
		for _, table := range []string{"pulse_reward_definition", "pulse_reward_budget"} {
			sqlDB.Exec("DELETE FROM "+table+" WHERE period_id=(SELECT id FROM pulse_period WHERE period_key=?)", key)
		}
		sqlDB.Exec("DELETE FROM pulse_period WHERE period_key=?", key)
	})
}
