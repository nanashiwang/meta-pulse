package notification

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/apache/answer/internal/base/constant"
	"github.com/apache/answer/internal/base/data"
	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/internal/schema"
	"github.com/apache/answer/internal/service/badge"
	"github.com/apache/answer/internal/service/notice_queue"
	nc "github.com/apache/answer/internal/service/notification_common"
	"github.com/segmentfault/pacman/contrib/cache/memory"
)

type badgeNoticeRepo struct {
	nc.NotificationRepo
	row *entity.Notification
	err error
}

func (r badgeNoticeRepo) GetById(context.Context, string) (*entity.Notification, bool, error) {
	return r.row, r.row != nil, r.err
}

type badgeQueue struct {
	notice_queue.NotificationQueueService
}

func (badgeQueue) RegisterHandler(func(context.Context, *schema.NotificationMsg) error) {}

type badgeLookup struct {
	badge.BadgeRepo
	calls *int
}

func (r badgeLookup) GetByID(context.Context, string) (*entity.Badge, bool, error) {
	*r.calls++
	return nil, false, nil
}
func TestMetarBadgeCacheValidation(t *testing.T) {
	for _, tt := range []struct {
		name string
		row  *entity.Notification
		err  error
		keep bool
	}{
		{"unread", &entity.Notification{UserID: "u", IsRead: schema.NotificationNotRead, Status: schema.NotificationStatusNormal}, nil, true},
		{"read", &entity.Notification{UserID: "u", IsRead: schema.NotificationRead, Status: schema.NotificationStatusNormal}, nil, false},
		{"missing", nil, nil, false},
		{"other owner", &entity.Notification{UserID: "other", IsRead: schema.NotificationNotRead, Status: schema.NotificationStatusNormal}, nil, false},
		{"deleted", &entity.Notification{UserID: "u", IsRead: schema.NotificationNotRead, Status: 0}, nil, false},
		{"database error", nil, errors.New("offline"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			d := &data.Data{Cache: memory.NewCache()}
			repo := badgeNoticeRepo{row: tt.row, err: tt.err}
			common := nc.NewNotificationCommon(d, repo, nil, nil, nil, nil, badgeQueue{}, nil, nil)
			if err := common.AddBadgeAwardAlertCache(ctx, "u", "n", "b"); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s := &NotificationService{data: d, notificationRepo: repo, notificationCommon: common, badgeRepo: badgeLookup{calls: &calls}}
			for i := 0; i < 100; i++ {
				s.getBadgeAward(ctx, "u")
			}
			_, exists, err := d.Cache.GetString(ctx, fmt.Sprintf(constant.RedDotCacheKey, constant.NotificationTypeBadgeAchievement, "u"))
			if err != nil || exists != tt.keep {
				t.Fatalf("cache exists=%v err=%v", exists, err)
			}
			if tt.name == "unread" && calls != 100 {
				t.Fatal("new unread badge suppressed")
			}
			if tt.name != "unread" && calls != 0 {
				t.Fatal("stale badge exposed")
			}
		})
	}
}
