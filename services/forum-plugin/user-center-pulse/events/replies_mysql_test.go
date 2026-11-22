package events

import (
	"context"
	"testing"

	"github.com/apache/answer/pkg/uid"
)

func TestMySQLRepliedThreadsUseLatestVisibleOwnReply(t *testing.T) {
	s := fixture(t)
	for _, sql := range []string{
		"DROP TABLE IF EXISTS question, answer, comment",
		"CREATE TABLE question(id BIGINT PRIMARY KEY,title VARCHAR(100),status INT,`show` INT)",
		"CREATE TABLE answer(id BIGINT PRIMARY KEY,question_id BIGINT,user_id BIGINT,status INT,created_at DATETIME,parsed_text TEXT)",
		"CREATE TABLE comment(id BIGINT PRIMARY KEY,question_id BIGINT,object_id BIGINT,user_id BIGINT,status INT,created_at DATETIME,parsed_text TEXT)",
		"INSERT INTO question VALUES (10010000000000001,'First thread',1,1),(10010000000000002,'Other thread',2,1),(10010000000000003,'Hidden thread',1,2),(10010000000000004,'Private author',11,1)",
		"INSERT INTO answer VALUES (10020000000000001,10010000000000001,2,1,'2026-11-20 10:00:00','<p>Old answer</p>'),(10020000000000002,10010000000000002,2,1,'2026-11-20 09:00:00','<p>Second answer</p>'),(10020000000000003,10010000000000002,2,10,'2026-11-22 09:00:00','deleted answer')",
		"INSERT INTO comment VALUES (10030000000000001,10010000000000001,10020000000000001,2,1,'2026-11-21 10:00:00','<b>Latest visible</b>'),(10030000000000002,10010000000000002,10020000000000003,2,1,'2026-11-22 10:00:00','hidden parent'),(10030000000000003,10010000000000002,10010000000000002,2,10,'2026-11-22 12:00:00','deleted comment'),(10030000000000004,10010000000000003,10010000000000003,2,1,'2026-11-22 10:00:00','hidden thread'),(10030000000000005,10010000000000001,10010000000000001,3,1,'2026-11-23 10:00:00','someone else'),(10030000000000006,10010000000000004,10010000000000004,2,1,'2026-11-22 10:00:00','pending thread')",
	} {
		if _, err := s.DB.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	result, err := s.RepliedThreads(ctx, "2", "", 1)
	if err != nil || len(result.Items) != 2 || result.More {
		t.Fatalf("%+v %v", result, err)
	}
	first, second := result.Items[0], result.Items[1]
	if first.Preview != "Latest visible" || first.AnswerID != uid.EnShortID("10020000000000001") || first.CommentID != uid.EnShortID("10030000000000001") || second.Preview != "Second answer" || second.CommentID != "" {
		t.Fatalf("%+v %+v", first, second)
	}
	selected, err := s.RepliedThreads(ctx, "2", "OTHER", 1)
	if err != nil || len(selected.Items) != 1 || selected.Items[0].Title != "Other thread" {
		t.Fatalf("%+v %v", selected, err)
	}
	other, err := s.RepliedThreads(ctx, "3", "", 1)
	if err != nil || len(other.Items) != 1 || other.Items[0].Preview != "someone else" || other.Items[0].AnswerID != "" {
		t.Fatalf("%+v %v", other, err)
	}
}
