package communitytickets

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestRandomFlashCentsBounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		c, err := RandomFlashCents()
		if err != nil || c < 1 || c > 150 {
			t.Fatal(c, err)
		}
	}
}
func TestMySQLFlashStoredBeforeDeliveryAndNeverRerolled(t *testing.T) {
	s, now := fixture(t)
	ctx := context.Background()
	*now = time.Date(2026, 11, 18, 1, 0, 0, 0, time.UTC)
	must(t, s.DB, "ALTER TABLE metar_exp_event ADD earned_day CHAR(10), ADD status VARCHAR(16)")
	must(t, s.DB, "INSERT INTO metar_exp_event(id,user_id,kind,occurred_at,earned_day,status) VALUES(1,2,'checkin',?,'2026-11-18','awarded'),(2,3,'checkin',?,'2026-11-18','awarded'),(3,4,'checkin',?,'2026-11-18','awarded')", now.Unix(), now.Unix(), now.Unix())
	if err := s.CollectFlash(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.Flash(ctx, "2", "2026-11-18")
	if err != nil || first.Cents < 1 || first.Cents > 150 || first.GrantID != "" || first.Status != "pending" {
		t.Fatal(first, err)
	}
	for i := 0; i < 100; i++ {
		if err = s.PrepareFlash(ctx, "2"); err != nil {
			t.Fatal(err)
		}
	}
	again, err := s.Flash(ctx, "2", "2026-11-18")
	if err != nil || again != first {
		t.Fatal("rerolled", again, err)
	}
	for _, user := range []string{"3", "4"} {
		if _, err = s.Flash(ctx, user, "2026-11-18"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("unbound/inactive eligible", user, err)
		}
	}
	if err = s.SaveFlash(ctx, first, "pg_one", "pending", first.Cents); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveFlash(ctx, first, "pg_one", "settled", first.Cents); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveFlash(ctx, first, "pg_one", "pending", first.Cents); err != nil {
		t.Fatal(err)
	}
	settled, err := s.Flash(ctx, "2", "2026-11-18")
	if err != nil || settled.Status != "settled" {
		t.Fatal("stale response regressed", settled, err)
	}
	if err = s.SaveFlash(ctx, first, "pg_one", "settled", first.Cents+1); err == nil {
		t.Fatal("amount mismatch accepted")
	}
	*now = now.AddDate(0, 0, 1)
	must(t, s.DB, "INSERT INTO metar_exp_event(id,user_id,kind,occurred_at,earned_day,status) VALUES(4,2,'checkin',?,'2026-11-19','awarded')", now.Unix())
	if err = s.CollectFlash(ctx); err != nil {
		t.Fatal(err)
	}
	next, err := s.Flash(ctx, "2", "2026-11-19")
	if err != nil || next.EventID == first.EventID {
		t.Fatal(next, err)
	}
}
