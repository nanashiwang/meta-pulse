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
	gate := int64(1)
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
	// Seed the zero outcome to verify replay deterministically regardless of the random generator.
	if _, err := s.DB.Exec("INSERT INTO user_external_login(user_id,external_id,provider) VALUES(3,43,'pulse_user_center')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("INSERT INTO metar_gift_claim(round_id,user_id,external_id,flash,exp,tickets,no_ticket_award,created_at) VALUES(?,3,43,0,0,0,1,?)", r.ID, s.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if o, v, err := s.Gift(ctx, "3", r.ID, nil); err != nil || o != nil || v == nil || v.Tickets != 0 || !v.NoTicketAward {
		t.Fatalf("zero result should replay as an already recorded claim: %+v %+v %v", o, v, err)
	}
	if o, v, err := s.Gift(ctx, "3", "", nil); err != nil || o != nil || v != nil {
		t.Fatalf("zero result should hide entry for members: %+v %+v %v", o, v, err)
	}
	rows, err := s.GiftReport(ctx, "1", r.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	people := rows.([]GiftPerson)
	if len(people) != 3 || !people[2].NoTicketAward || people[2].Tickets != 0 {
		t.Fatalf("zero result missing from administrator report: %+v", people)
	}

	s.Now = func() time.Time { return time.Unix(1900000000, 0) }
	if o, receipt, err := s.Gift(ctx, "1", "", nil); err != nil || o != nil || receipt != nil {
		t.Fatalf("finished rounds must not remain in nav: %+v %+v %v", o, receipt, err)
	}
	if offer, _, e := s.Gift(ctx, "2", "", get); e != nil || offer != nil {
		t.Fatal(fmt.Sprint(offer, e))
	}
}

func TestMySQLGiftRuleCapacityFallsThroughUnderConcurrency(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, q := range []string{
		"DELETE FROM metar_gift_claim", "DELETE FROM metar_gift_round", "DROP TABLE IF EXISTS user_external_login",
		"CREATE TABLE user_external_login(user_id BIGINT PRIMARY KEY,external_id BIGINT,provider VARCHAR(50),meta_pulse_user_id_guard BIGINT GENERATED ALWAYS AS (IF(provider='pulse_user_center',user_id,NULL)) STORED UNIQUE)",
		"INSERT INTO user_external_login(user_id,external_id,provider) VALUES(1,41,'pulse_user_center'),(2,42,'pulse_user_center'),(3,43,'pulse_user_center')",
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Execute(ctx, "1", "capacity-gift", Command{Op: "gift_start", Gift: &GiftRules{Name: "capacity", Minutes: 5, Groups: []GiftRule{
		{UsageRaw: "100", MaxClaims: giftNumber(1), Exp: &GiftRange{10, 10}},
		{MaxClaims: giftNumber(1), Exp: &GiftRange{1, 1}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		user    string
		receipt *GiftReceipt
		err     error
	}
	ready, release := make(chan struct{}, 2), make(chan struct{})
	get := func(c context.Context, _, _ string) (*int64, error) {
		select {
		case ready <- struct{}{}:
		case <-c.Done():
			return nil, c.Err()
		}
		select {
		case <-release:
			return giftNumber(101), nil
		case <-c.Done():
			return nil, c.Err()
		}
	}
	got := make(chan result, 2)
	for _, user := range []string{"2", "3"} {
		go func(user string) {
			_, receipt, err := s.Gift(ctx, user, r.ID, get)
			got <- result{user, receipt, err}
		}(user)
	}
	// Both requests see the first rule as available before either starts claiming.
	for i := 0; i < 2; i++ {
		select {
		case <-ready:
		case v := <-got:
			t.Fatalf("claim returned before both checks started: %+v", v)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	seen := make(map[int]int64)
	for i := 0; i < 2; i++ {
		v := <-got
		if v.err != nil || v.receipt == nil {
			t.Fatalf("missing claim for %s: %v", v.user, v.err)
		}
		seen[v.receipt.RuleNumber] = v.receipt.Exp
		_, replay, err := s.Gift(ctx, v.user, r.ID, nil)
		if err != nil || replay == nil || *replay != *v.receipt {
			t.Fatalf("claim changed on replay: %+v, %v", replay, err)
		}
	}
	if len(seen) != 2 || seen[1] != 10 || seen[2] != 1 {
		t.Fatalf("expected one winner per rule, got %v", seen)
	}
	if offer, receipt, err := s.Gift(ctx, "1", "", nil); err != nil || offer == nil || !offer.Locked || offer.Unavailable || receipt != nil {
		t.Fatalf("full rules should be skipped without a usage lookup: %+v, %+v, %v", offer, receipt, err)
	}
	if offer, receipt, err := s.Gift(ctx, "1", r.ID, nil); err != nil || offer != nil || receipt != nil {
		t.Fatalf("all rules full should prevent claiming: %+v, %+v, %v", offer, receipt, err)
	}
	detail, err := s.GiftReport(ctx, "1", r.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	rows := detail.([]GiftPerson)
	if len(rows) != 2 || rows[0].RuleNumber+rows[1].RuleNumber != 3 {
		t.Fatalf("expected both matched rules in the administrator report: %+v", rows)
	}
}

func TestMySQLGiftRuleNumberMigrationPreservesLegacyClaims(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, q := range []string{
		"DELETE FROM metar_gift_claim", "DELETE FROM metar_gift_round",
		"ALTER TABLE metar_gift_claim DROP INDEX gift_rule_claims, DROP COLUMN rule_number",
		"INSERT INTO metar_gift_claim(round_id,user_id,external_id,flash,exp,tickets,flash_paid,created_at) VALUES(1,2,42,125,0,0,1,1800000000)",
	} {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	// Ensure is reentrant when the column and index have already been added.
	for i := 0; i < 2; i++ {
		if err := s.Ensure(ctx); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.GiftReport(ctx, "1", "1", 1)
	if err != nil {
		t.Fatal(err)
	}
	rows := report.([]GiftPerson)
	if len(rows) != 1 || rows[0].RuleNumber != 0 || rows[0].Flash != 125 || !rows[0].FlashPaid {
		t.Fatalf("legacy claim changed after upgrade: %+v", rows)
	}
}

func TestMySQLGiftTierHistoryUsesFrozenRulesAndFiltersBeforePagination(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	for _, q := range []string{"DELETE FROM metar_gift_claim", "DELETE FROM metar_gift_round"} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Execute(ctx, "1", "history-tier-test", Command{Op: "gift_start", Gift: &GiftRules{Name: "tier history", Minutes: 5, Groups: []GiftRule{
		{Likes: giftNumber(3), Comments: giftNumber(0), UsageRaw: "9007199254740993", MaxClaims: giftNumber(55), Flash: &GiftRange{100, 199}, Tickets: &GiftRange{0, 2}},
		{Exp: &GiftRange{5, 5}},
		{Exp: &GiftRange{1, 1}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	seed := func(i, n, flash, exp, tickets int, paid, zero bool) {
		t.Helper()
		if _, err := s.DB.Exec("INSERT INTO metar_gift_claim(round_id,user_id,external_id,rule_number,flash,exp,tickets,flash_paid,exp_paid,tickets_paid,no_ticket_award,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, i, 10000+i, n, flash, exp, tickets, paid, paid, paid, zero, s.Now().Unix()); err != nil {
			t.Fatal(err)
		}
	}
	seed(2, 2, 0, 5, 0, false, false)
	seed(3, 0, 200, 0, 0, true, false)
	for user := 100; user <= 151; user++ {
		seed(user, 1, 155, 0, 0, user%2 == 0, true)
	}
	s.Now = func() time.Time { return time.Unix(1900000000, 0) }
	report, err := s.GiftTierReport(ctx, "1", r.ID)
	if err != nil || len(report.Tiers) != 3 || report.Name != "tier history" || !report.SocialInclusive {
		t.Fatal(report, err)
	}
	a, b, c := report.Tiers[0], report.Tiers[1], report.Tiers[2]
	if a.Number != 1 || a.People != 52 || a.Flash != 26*155 || a.Rule.UsageRaw != "9007199254740993" || *a.Rule.MaxClaims != 55 || a.Rule.Tickets.Min != 0 || b.People != 1 || b.Exp != 0 || c.People != 0 || report.Unassigned.People != 1 || report.Unassigned.Flash != 200 {
		t.Fatalf("incorrect tier configuration or totals: %+v", report)
	}
	for _, tc := range []struct {
		tier, page, count int
		first             string
	}{{1, 1, 50, "100"}, {1, 2, 2, "150"}, {2, 1, 1, "2"}, {0, 1, 1, "3"}, {3, 1, 0, ""}} {
		rows, err := s.GiftTierPeople(ctx, "1", r.ID, tc.page, tc.tier)
		if err != nil || len(rows) != tc.count {
			t.Fatalf("tier %d page %d: %v %v", tc.tier, tc.page, rows, err)
		}
		if len(rows) > 0 && (rows[0].RuleNumber != tc.tier || rows[0].User != tc.first || (tc.tier == 1 && !rows[0].NoTicketAward)) {
			t.Fatalf("wrong claimant: %+v", rows[0])
		}
	}
	if _, err := s.GiftTierReport(ctx, "2", r.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.GiftTierPeople(ctx, "2", r.ID, 1, 1); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if _, err := s.GiftTierPeople(ctx, "1", r.ID, 1, 4); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
