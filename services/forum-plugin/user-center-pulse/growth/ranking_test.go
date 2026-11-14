package growth

import (
	"context"
	"fmt"
	"testing"
)

func TestRankingRejectsUnknownKind(t *testing.T) {
	for _, kind := range []string{"", "reputation", "posts; DROP TABLE user"} {
		if _, err := rankingQuery(kind); err != ErrInvalid {
			t.Fatalf("kind %q: %v", kind, err)
		}
	}
}

func TestPublicRankingsMySQL(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	for _, q := range []string{"ALTER TABLE user ADD display_name VARCHAR(60) DEFAULT '', ADD avatar TEXT", "INSERT INTO metar_exp_account(user_id,balance) VALUES(1,999999),(2,4500),(3,600),(4,99999)", "INSERT INTO question(id,user_id,status,`show`,created_at) VALUES(10,2,1,1,NOW()),(11,2,2,1,NOW()),(12,2,1,0,NOW()),(13,2,3,1,NOW()),(14,3,1,1,NOW())", "INSERT INTO answer(id,user_id,question_id,status,created_at) VALUES(20,2,10,1,NOW()),(21,2,12,1,NOW()),(22,2,10,10,NOW()),(23,3,14,1,NOW()),(24,3,10,1,NOW())"} {
		if _, err := s.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	levels, err := s.Ranking(ctx, "level")
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 2 || levels[0].Username != "member" || levels[0].Level != 4 {
		t.Fatalf("levels: %+v", levels)
	}
	posts, err := s.Ranking(ctx, "posts")
	if err != nil {
		t.Fatal(err)
	}
	if len(posts) != 2 || posts[0].Value != 2 || posts[0].Username != "member" {
		t.Fatalf("posts: %+v", posts)
	}
	answers, err := s.Ranking(ctx, "answers")
	if err != nil {
		t.Fatal(err)
	}
	if len(answers) != 2 || answers[0].Username != "voter" || answers[0].Value != 2 || answers[1].Value != 1 {
		t.Fatalf("answers: %+v", answers)
	}
	for i := 10; i < 120; i++ {
		if _, err = s.DB.Exec("INSERT INTO user(id,username,status,mail_status,created_at) VALUES(?,?,1,1,NOW())", i, fmt.Sprintf("user%d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err = s.DB.Exec("INSERT INTO metar_exp_account(user_id,balance) VALUES(?,100)", i); err != nil {
			t.Fatal(err)
		}
	}
	levels, err = s.Ranking(ctx, "level")
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 100 || levels[2].Username != "user10" || levels[99].Username != "user107" {
		t.Fatalf("unexpected top100: %+v", levels)
	}
}
