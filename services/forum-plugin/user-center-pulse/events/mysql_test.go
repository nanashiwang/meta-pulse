package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("EVENTS_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("EVENTS_INTEGRATION_DSN required")
	}
	cfg, e := mysql.ParseDSN(dsn)
	if e != nil || !strings.HasSuffix(cfg.DBName, "_events_test") {
		t.Fatal("dedicated _events_test database required")
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, name := range []string{"metar_event_actor", "metar_event_member", "metar_event_request", "metar_event", "user_role_rel", "user"} {
		if _, e = db.Exec("DROP TABLE IF EXISTS `" + name + "`"); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []string{"CREATE TABLE `user`(id BIGINT PRIMARY KEY,username VARCHAR(30),display_name VARCHAR(30),status INT,mail_status INT)", "CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)", "INSERT INTO `user` VALUES(1,'admin','Admin',1,1),(2,'alice','Alice',1,1),(3,'bob','Bob',1,1),(4,'inactive','Inactive',1,2),(5,'suspended','Suspended',2,1)", "INSERT INTO user_role_rel VALUES(1,2)"} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	s := New(db)
	s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	if e = s.Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}
func event() Event {
	return Event{Title: "Community workshop", Body: "Free session", Location: "Online", Starts: 1800003600, Ends: 1800007200, Deadline: 1800003000, Capacity: 1}
}
func createEvent(t *testing.T, s *Store) Result {
	t.Helper()
	r, e := s.Execute(context.Background(), "1", "create-key", Command{Op: "create", Event: event()})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestMySQLRegistrationCapacityAndReplays(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := createEvent(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u := "2"
			if i%2 == 1 {
				u = "3"
			}
			_, e := s.Execute(ctx, u, "join-key-"+u, Command{Op: "join", Event: Event{ID: r.ID}})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrFull) {
			t.Fatal(e)
		}
	}
	if success != 50 {
		t.Fatalf("successful retries %d", success)
	}
	page, e := s.List(ctx, "", 1, false)
	if e != nil || len(page.Items) != 1 || page.Items[0].Count != 1 || page.Items[0].Joined {
		t.Fatal(page, e)
	}
	roster, e := s.Roster(ctx, "1", r.ID, 1)
	if e != nil || len(roster) != 1 {
		t.Fatal(roster, e)
	}
	winner := "2"
	if roster[0].Username == "bob" {
		winner = "3"
	}
	loser := "3"
	if winner == "3" {
		loser = "2"
	}
	if _, e = s.Execute(ctx, winner, "join-key-"+winner, Command{Op: "leave", Event: Event{ID: r.ID}}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	for i := 0; i < 100; i++ {
		if _, e = s.Execute(ctx, winner, "leave-key", Command{Op: "leave", Event: Event{ID: r.ID}}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Execute(ctx, loser, "join-after-leave", Command{Op: "join", Event: Event{ID: r.ID}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, winner, "join-key-"+winner, Command{Op: "join", Event: Event{ID: r.ID}}); e != nil {
		t.Fatal(e)
	} // receipt cannot re-register after a withdrawal
	page, e = s.List(ctx, winner, 1, false)
	if e != nil || page.Items[0].Joined || page.Items[0].Count != 1 {
		t.Fatal(page, e)
	}
}
func TestMySQLEventPermissionsAndCancellation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := createEvent(t, s)
	for _, u := range []string{"2", "4", "5"} {
		if _, e := s.Execute(ctx, u, "admin-create", Command{Op: "create", Event: event()}); !errors.Is(e, ErrForbidden) {
			t.Fatal(u, e)
		}
	}
	for _, u := range []string{"4", "5"} {
		if _, e := s.Execute(ctx, u, "member-join", Command{Op: "join", Event: Event{ID: r.ID}}); !errors.Is(e, ErrForbidden) {
			t.Fatal(u, e)
		}
	}
	if _, e := s.Roster(ctx, "2", r.ID, 1); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e := s.Execute(ctx, "2", "join-key", Command{Op: "join", Event: Event{ID: r.ID}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute(ctx, "1", "cancel-key", Command{Op: "cancel", Event: Event{ID: r.ID, Version: 1}}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute(ctx, "3", "join-closed", Command{Op: "join", Event: Event{ID: r.ID}}); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
	page, e := s.List(ctx, "2", 1, false)
	if e != nil || !page.Items[0].Joined || page.Items[0].Status != "cancelled" {
		t.Fatal(page, e)
	}
	s.DB.Exec("DELETE FROM user_role_rel WHERE user_id=1")
	if _, e = s.Execute(ctx, "1", "cancel-key", Command{Op: "cancel", Event: Event{ID: r.ID, Version: 1}}); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.Roster(ctx, "1", r.ID, 1); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
}
func TestMySQLDatesEditingPaginationAndLostResponse(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r := createEvent(t, s)
	repeat, e := s.Execute(ctx, "1", "create-key", Command{Op: "create", Event: event()})
	if e != nil || repeat != r {
		t.Fatal(repeat, e)
	}
	v := event()
	v.ID = r.ID
	v.Version = 1
	v.Title = "Updated"
	if _, e = s.Execute(ctx, "1", "edit-key", Command{Op: "edit", Event: v}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Execute(ctx, "1", "stale-edit", Command{Op: "edit", Event: v}); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	for i := 0; i < 21; i++ {
		if _, e = s.Execute(ctx, "1", fmt.Sprintf("event-key-%d", i), Command{Op: "create", Event: event()}); e != nil {
			t.Fatal(e)
		}
	}
	first, e := s.List(ctx, "", 1, false)
	if e != nil || len(first.Items) != 20 || !first.More {
		t.Fatal(first, e)
	}
	second, e := s.List(ctx, "", 2, false)
	if e != nil || len(second.Items) != 2 || second.More {
		t.Fatal(second, e)
	}
	s.Now = func() time.Time { return time.Unix(1800003000, 0) }
	if _, e = s.Execute(ctx, "2", "deadline-join", Command{Op: "join", Event: Event{ID: r.ID}}); !errors.Is(e, ErrClosed) {
		t.Fatal(e)
	}
}
