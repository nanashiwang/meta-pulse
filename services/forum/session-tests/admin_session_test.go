package auth

import (
	"context"
	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"sync"
	"testing"
	"time"
)

func adminFixture(t *testing.T) (*authRepo, *clockCache) {
	t.Helper()
	r, c, ctx := fixture(t)
	db, err := data.NewDB(false, &data.Database{Driver: "sqlite", Connection: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r.data.DB = db
	if err = db.Sync2(new(entity.User)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE user_role_rel(user_id BIGINT,role_id INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Insert(&entity.User{ID: "7", Status: 1, MailStatus: 1, Username: "administrator", EMail: "admin@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO user_role_rel VALUES(7,2)"); err != nil {
		t.Fatal(err)
	}
	if err = r.SetAdminUserCacheInfo(ctx, "token", &entity.UserCacheInfo{UserID: "7", UserStatus: 1, EmailStatus: 1, RoleID: 2}); err != nil {
		t.Fatal(err)
	}
	return r, c
}
func TestMetarAdminFollowsCommunitySessionWithoutSeparateExpiry(t *testing.T) {
	r, c := adminFixture(t)
	ctx := context.Background()
	c.Del(ctx, constant.AdminTokenCacheKey+"token") // Existing seven-day cache may already be gone.
	for i := 0; i < 3; i++ {
		c.now = c.now.Add(29 * 24 * time.Hour)
		u, err := r.GetAdminUserCacheInfo(ctx, "token")
		if err != nil || u == nil || u.RoleID != 2 {
			t.Fatalf("day %d: %+v %v", (i+1)*29, u, err)
		}
	}
	c.now = c.now.Add(30 * 24 * time.Hour)
	if u, err := r.GetAdminUserCacheInfo(ctx, "token"); err != nil || u != nil {
		t.Fatal("expired community bearer revived", u, err)
	}
}
func TestMetarAdminChecksCurrentRoleAndAccount(t *testing.T) {
	for _, query := range []string{"UPDATE user_role_rel SET role_id=1", "UPDATE user_role_rel SET role_id=3", "DELETE FROM user_role_rel", "INSERT INTO user_role_rel VALUES(7,2)", "UPDATE user SET status=9", "UPDATE user SET status=10", "UPDATE user SET mail_status=2"} {
		t.Run(query, func(t *testing.T) {
			r, _ := adminFixture(t)
			ctx := context.Background()
			if _, err := r.data.DB.Exec(query); err != nil {
				t.Fatal(err)
			}
			if u, err := r.GetAdminUserCacheInfo(ctx, "token"); err != nil || u != nil {
				t.Fatal("invalid admin allowed", u, err)
			}
		})
	}
}
func TestMetarAdminRevocationAndUnavailableDatabase(t *testing.T) {
	t.Run("logout", func(t *testing.T) {
		r, _ := adminFixture(t)
		ctx := context.Background()
		r.RemoveUserCacheInfo(ctx, "token")
		if u, _ := r.GetAdminUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("logout revived")
		}
	})
	t.Run("password reset", func(t *testing.T) {
		r, _ := adminFixture(t)
		ctx := context.Background()
		r.RemoveUserTokens(ctx, "7", "")
		if u, _ := r.GetAdminUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("password reset revived")
		}
	})
	t.Run("missing bearer", func(t *testing.T) {
		r, c := adminFixture(t)
		ctx := context.Background()
		c.Del(ctx, constant.UserTokenCacheKey+"token")
		if u, _ := r.GetAdminUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("admin cache revived bearer")
		}
	})
	t.Run("database failure", func(t *testing.T) {
		r, _ := adminFixture(t)
		ctx := context.Background()
		r.data.DB.Close()
		if u, err := r.GetAdminUserCacheInfo(ctx, "token"); err == nil || u != nil {
			t.Fatal("database failed open")
		}
	})
	t.Run("concurrent logout", func(t *testing.T) {
		r, _ := adminFixture(t)
		ctx := context.Background()
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); r.GetAdminUserCacheInfo(ctx, "token") }()
		}
		r.RemoveUserCacheInfo(ctx, "token")
		wg.Wait()
		if u, _ := r.GetAdminUserCacheInfo(ctx, "token"); u != nil {
			t.Fatal("concurrent session revival")
		}
	})
}
