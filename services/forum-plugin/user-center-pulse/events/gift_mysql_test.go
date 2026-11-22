package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMySQLGiftConcurrentClaimAndReport(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM metar_gift_claim", "DELETE FROM metar_gift_round", "DROP TABLE IF EXISTS user_external_login", "DROP TABLE IF EXISTS activity,config,comment", "CREATE TABLE activity(user_id BIGINT,trigger_user_id BIGINT,cancelled BOOL,activity_type BIGINT,created_at DATETIME)", "CREATE TABLE config(id INT,`key` VARCHAR(50))", "INSERT INTO config VALUES(1,'answer.voted_up')", "CREATE TABLE comment(user_id BIGINT,status INT,created_at DATETIME)", "CREATE TABLE user_external_login(user_id BIGINT PRIMARY KEY,external_id BIGINT,provider VARCHAR(50),meta_pulse_user_id_guard BIGINT GENERATED ALWAYS AS (IF(provider='pulse_user_center',user_id,NULL)) STORED UNIQUE)", "INSERT INTO user_external_login(user_id,external_id,provider) VALUES(2,42,'pulse_user_center')"} {
		if _, e := s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	gate := int64(0)
	cmd := Command{Op: "gift_start", Gift: &GiftRules{Name: "gift", Minutes: 5, Groups: []GiftRule{
		{UsageRaw: "15000", Likes: &gate, Comments: &gate, Flash: &GiftRange{1, 50}, Exp: &GiftRange{2, 2}, Tickets: &GiftRange{1, 1}},
		{Likes: &gate, Comments: &gate, Exp: &GiftRange{100, 100}, Tickets: &GiftRange{10, 10}},
	}}}
	if _, e := s.Execute(ctx, "2", "gift-admin-key", cmd); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	r, e := s.Execute(ctx, "1", "gift-start-key", cmd)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, "1", "gift-other-key", cmd); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if o, receipt, e := s.Gift(ctx, "1", "", nil); e != nil || o == nil || !o.Locked || o.ClaimedCount == nil || *o.ClaimedCount != 0 || receipt != nil {
		t.Fatalf("unbound admin should see the locked round: %+v %+v %v", o, receipt, e)
	}
	if o, receipt, e := s.Gift(ctx, "1", r.ID, nil); e != nil || o != nil || receipt != nil {
		t.Fatalf("view permission must not allow claiming: %+v %+v %v", o, receipt, e)
	}
	calls := 0
	raw := int64(15000)
	get := func(_ context.Context, ex, day string) (*int64, error) {
		calls++
		if ex != "42" || day != s.Now().In(giftZone).AddDate(0, 0, -1).Format("2006-01-02") {
			t.Fatal(ex, day)
		}
		return &raw, nil
	}
	if o, _, e := s.Gift(ctx, "3", "", get); e != nil || o != nil || calls != 0 {
		t.Fatal(o, calls, e)
	}
	if o, _, e := s.Gift(ctx, "2", "", get); e != nil || o != nil {
		t.Fatal(o, e)
	}
	raw++
	if o, _, e := s.Gift(ctx, "2", "", get); e != nil || o != nil {
		t.Fatal("missing interaction", e)
	}
	y := s.Now().In(giftZone).AddDate(0, 0, -1).Unix()
	if _, e = s.DB.Exec("INSERT INTO activity VALUES(9,2,0,1,FROM_UNIXTIME(?))", y); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO comment VALUES(2,1,FROM_UNIXTIME(?))", y); e != nil {
		t.Fatal(e)
	}
	if o, _, e := s.Gift(ctx, "2", "", get); e != nil || o == nil || o.ClaimedCount != nil || o.Locked {
		t.Fatal(o, e)
	}
	get = func(context.Context, string, string) (*int64, error) { return &raw, nil }
	var wg sync.WaitGroup
	got := make(chan int64, 50)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, v, e := s.Gift(ctx, "2", r.ID, get)
			if e != nil || v == nil {
				t.Error(e)
				return
			}
			got <- v.Flash
		}()
	}
	wg.Wait()
	close(got)
	var amount int64
	for n := range got {
		if amount != 0 && n != amount || n < 1 || n > 50 {
			t.Fatal(n)
		}
		amount = n
	}
	if o, _, e := s.Gift(ctx, "2", "", get); e != nil || o != nil {
		t.Fatal("entry after claim", e)
	}
	report, e := s.GiftReport(ctx, "1", "", 1)
	if e != nil {
		t.Fatal(e)
	}
	v := report.([]GiftReport)[0]
	if v.People != 1 || v.Flash != 0 || v.Exp != 0 || v.Tickets != 0 || v.Actor != "Admin" {
		t.Fatal(v)
	}
	if _, e = s.DB.Exec("UPDATE metar_gift_claim SET flash_paid=1,exp_paid=1 WHERE round_id=?", r.ID); e != nil {
		t.Fatal(e)
	}
	report, e = s.GiftReport(ctx, "1", "", 1)
	if e != nil || report.([]GiftReport)[0].Flash != amount {
		t.Fatal(report, e)
	}
	detail, e := s.GiftReport(ctx, "1", r.ID, 1)
	if e != nil || len(detail.([]GiftPerson)) != 1 || detail.([]GiftPerson)[0].Name != "Alice" {
		t.Fatal(detail, e)
	}
	if _, e = s.GiftReport(ctx, "2", r.ID, 1); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if o, receipt, e := s.Gift(ctx, "1", "", nil); e != nil || o == nil || !o.Locked || o.ClaimedCount == nil || *o.ClaimedCount != 1 || receipt != nil {
		t.Fatalf("admin should see the round and the member count but not the member receipt: %+v %+v %v", o, receipt, e)
	}
	if _, err := s.DB.Exec("INSERT INTO user_external_login(user_id,external_id,provider) VALUES(1,41,'pulse_user_center')"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"INSERT INTO activity VALUES(9,1,0,1,FROM_UNIXTIME(?))", "INSERT INTO comment VALUES(1,1,FROM_UNIXTIME(?))"} {
		if _, err := s.DB.Exec(q, y); err != nil {
			t.Fatal(err)
		}
	}

	if o, receipt, err := s.Gift(ctx, "1", "", nil); err != nil || o == nil || !o.Locked || !o.Unavailable || receipt != nil {
		t.Fatalf("temporarily unavailable qualification must stay locked: %+v %+v %v", o, receipt, err)
	}
	_, saved, err := s.Gift(ctx, "1", r.ID, get)
	if err != nil || saved == nil {
		t.Fatalf("eligible admin should be able to claim: %+v %v", saved, err)
	}
	if o, receipt, err := s.Gift(ctx, "1", "", nil); err != nil || o == nil || o.Locked || o.ClaimedCount == nil || *o.ClaimedCount != 2 || receipt == nil || *receipt != *saved {
		t.Fatalf("admin should retain access and see only the original personal receipt: %+v %+v %v", o, receipt, err)
	}
	s.Now = func() time.Time { return time.Unix(1900000000, 0) }
	if o, receipt, err := s.Gift(ctx, "1", "", nil); err != nil || o != nil || receipt != nil {
		t.Fatalf("finished rounds must not remain in nav: %+v %+v %v", o, receipt, err)
	}
	if offer, _, e := s.Gift(ctx, "2", "", get); e != nil || offer != nil {
		t.Fatal(fmt.Sprint(offer, e))
	}
}
