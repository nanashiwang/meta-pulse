package marketplace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	mysql "github.com/go-sql-driver/mysql"
)

func fixture(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("MARKET_INTEGRATION_DSN")
	if dsn == "" {
		t.Skip("MARKET_INTEGRATION_DSN required")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_market_test") {
		t.Fatal("dedicated _market_test database required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(12)
	for _, name := range []string{"metar_market_audit", "metar_market_request", "metar_market_actor", "metar_market_item", "question", "user_role_rel", "user"} {
		if _, err = db.Exec("DROP TABLE IF EXISTS `" + name + "`"); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"CREATE TABLE `user`(id BIGINT PRIMARY KEY,username VARCHAR(40),display_name VARCHAR(40),avatar VARCHAR(100) NOT NULL DEFAULT '',status INT,mail_status INT)",
		"CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,user_id BIGINT,title VARCHAR(255),status INT,`show` INT,answer_count INT NOT NULL DEFAULT 0,view_count INT NOT NULL DEFAULT 0)",
		"INSERT INTO `user`(id,username,display_name,status,mail_status) VALUES(1,'admin','Admin',1,1),(2,'alice','Alice',1,1),(3,'bob','Bob',1,1),(4,'inactive','Inactive',1,2),(5,'banned','Banned',2,1)",
		"INSERT INTO user_role_rel VALUES(1,2)",
		"INSERT INTO question(id,user_id,title,status,`show`) VALUES(12,2,'Prompt board',1,1),(13,2,'An idea',1,1),(14,3,'Other author',1,1),(15,2,'Second project',1,1)",
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db)
	if err = s.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}
func mustCommand(t *testing.T, s *Store, user, key string, c Command, admin bool) Item {
	t.Helper()
	i, e := s.Execute(context.Background(), user, key, c, admin)
	if e != nil {
		t.Fatalf("%s/%s: %v", user, c.Op, e)
	}
	return i
}
func publish(t *testing.T, s *Store, item Item) Item {
	t.Helper()
	return mustCommand(t, s, "1", "publish-"+item.ID+fmt.Sprint(item.Version), Command{Op: "review", ID: item.ID, Version: item.Version, Decision: "publish", Reason: "Reviewed provenance and usage", DemoStatus: "available", EmbedAllowed: item.Entry.EmbedRequested}, true)
}
func TestMySQLReviewLiveSnapshotAndVisibility(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	entry := validProject()
	entry.DemoURL = "https://demo.example.com/app"
	entry.EmbedRequested = true
	item := mustCommand(t, s, "2", "submit", Command{Op: "submit", Entry: entry}, false)
	if _, e := s.Get(ctx, item.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("unreviewed item visible", e)
	}
	item = publish(t, s, item)
	live, e := s.Get(ctx, item.ID)
	if e != nil || !live.EmbedAllowed {
		t.Fatal(live, e)
	}
	if _, e = s.Execute(ctx, "2", "unreviewed-save", Command{Op: "save_draft", ID: item.ID, Version: item.Version, Entry: entry}, false); !errors.Is(e, ErrConflict) {
		t.Fatal("published draft bypass", e)
	}
	changed := entry
	changed.DemoURL = "https://changed.example.com/app"
	update := mustCommand(t, s, "2", "update", Command{Op: "submit", ID: item.ID, Version: item.Version, Entry: changed}, false)
	live, e = s.Get(ctx, item.ID)
	if e != nil || live.Entry.DemoURL != entry.DemoURL {
		t.Fatal("pending address replaced live address", live, e)
	}
	if _, e = s.Execute(ctx, "2", "pending-overwrite", Command{Op: "submit", ID: update.ID, Version: update.Version, Entry: entry}, false); !errors.Is(e, ErrConflict) {
		t.Fatal("pending overwritten", e)
	}
	update = mustCommand(t, s, "1", "request-changes", Command{Op: "review", ID: update.ID, Version: update.Version, Decision: "changes", Reason: "Please clarify costs"}, true)
	live, e = s.Get(ctx, item.ID)
	if e != nil || live.Entry.DemoURL != entry.DemoURL {
		t.Fatal("rejection removed live entry", e)
	}
	update = mustCommand(t, s, "2", "revise", Command{Op: "submit", ID: update.ID, Version: update.Version, Entry: changed}, false)
	update = publish(t, s, update)
	live, e = s.Get(ctx, item.ID)
	if e != nil || live.Entry.DemoURL != changed.DemoURL {
		t.Fatal(live, e)
	}
	// Replays do not bypass current activation or moderation in public reads.
	if _, e = s.DB.Exec("UPDATE question SET `show`=2 WHERE id=12"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, item.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("moderated post remains visible", e)
	}
	if _, e = s.DB.Exec("UPDATE question SET `show`=1 WHERE id=12"); e != nil {
		t.Fatal(e)
	}
	mustCommand(t, s, "2", "archive", Command{Op: "archive", ID: update.ID, Version: update.Version}, false)
	if _, e = s.Get(ctx, item.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("archived item visible", e)
	}
}
func TestMySQLConcurrentReplayAndLostResponse(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	cmd := Command{Op: "submit", Entry: validProject()}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	ids := make(chan string, 100)
	for n := 0; n < 100; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			i, e := s.Execute(ctx, "2", "stable-request", cmd, false)
			errs <- e
			ids <- i.ID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for value := range ids {
		if id == "" {
			id = value
		}
		if id != value {
			t.Fatal("replay changed ID")
		}
	}
	for _, table := range []string{"metar_market_item", "metar_market_audit", "metar_market_request"} {
		var count int
		if e := s.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); e != nil || count != 1 {
			t.Fatal(table, count, e)
		}
	}
	changed := cmd
	changed.Entry.Summary = "Different payload"
	if _, e := s.Execute(ctx, "2", "stable-request", changed, false); !errors.Is(e, ErrConflict) {
		t.Fatal("key reused with different payload", e)
	}
	// A normal HTTP response loss replays the committed receipt.
	got, e := s.Execute(ctx, "2", "stable-request", cmd, false)
	if e != nil || got.ID != id {
		t.Fatal(got, e)
	}
	// A reserved but incomplete legacy request can finish in the same transaction.
	next := cmd
	next.Entry.QuestionID = "15"
	next.Entry, e = ValidateEntry(next.Entry, true)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(next)
	sum := sha256.Sum256(raw)
	if _, e = s.DB.Exec("INSERT INTO metar_market_request(actor_id,request_key,fingerprint,response,created_at) VALUES(2,'incomplete',?,'',0)", hex.EncodeToString(sum[:])); e != nil {
		t.Fatal(e)
	}
	resumed := mustCommand(t, s, "2", "incomplete", next, false)
	again := mustCommand(t, s, "2", "incomplete", next, false)
	if resumed.ID != again.ID {
		t.Fatal("incomplete request duplicated")
	}
}
func TestMySQLOwnershipRolesIdeasAndFilters(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	entry := validProject()
	for _, user := range []string{"3", "4", "5"} {
		if _, e := s.Execute(ctx, user, "not-owner", Command{Op: "submit", Entry: entry}, false); !errors.Is(e, ErrForbidden) {
			t.Fatal(user, e)
		}
	}
	idea := Entry{QuestionID: "13", Kind: "idea", Summary: "An idea for developers", UseCase: "development", Relation: "self", IdeaStage: "building"}
	i := publish(t, s, mustCommand(t, s, "2", "idea", Command{Op: "submit", Entry: idea}, false))
	entry.SourceIdeaID = i.ID
	p := publish(t, s, mustCommand(t, s, "2", "project", Command{Op: "submit", Entry: entry}, false))
	detail, e := s.Detail(ctx, i.ID)
	if e != nil || len(detail.Projects) != 1 || detail.Projects[0].ID != p.ID {
		t.Fatal(detail, e)
	}
	detail, e = s.Detail(ctx, p.ID)
	if e != nil || detail.Source == nil || detail.Source.ID != i.ID {
		t.Fatal(detail, e)
	}
	if _, e = s.Execute(ctx, "3", "review-bypass", Command{Op: "review", ID: p.ID, Version: p.Version, Decision: "archive", Reason: "not admin"}, true); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	page, e := s.List(ctx, Query{Page: 1, Kind: "project", Text: "prompt", UseCase: "development", Capability: "github"})
	if e != nil || page.Count != 1 {
		t.Fatal(page, e)
	}
	page, e = s.List(ctx, Query{Page: 1, Text: "%"})
	if e != nil || page.Count != 0 {
		t.Fatal("LIKE wildcard not escaped", page, e)
	}
	private, e := s.PrivateList(ctx, "3", Query{Page: 1}, false)
	if e != nil || private.Count != 0 {
		t.Fatal("other author drafts leaked", private, e)
	}
	if _, e = s.PrivateList(ctx, "3", Query{Page: 1}, true); !errors.Is(e, ErrForbidden) {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE `user` SET status=2 WHERE id=2"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Get(ctx, p.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("banned author still public", e)
	}
	if _, e = s.Execute(ctx, "2", "project", Command{Op: "submit", Entry: entry}, false); !errors.Is(e, ErrForbidden) {
		t.Fatal("banned actor receipt bypass", e)
	}
}
