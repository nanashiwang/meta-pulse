package pulse_user_center

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/answer/pkg/uid"
	"github.com/gin-gonic/gin"
)

type adminCommunityStatistics struct {
	RangeStart    string `json:"range_start"`
	RangeEnd      string `json:"range_end"`
	RangeComments int64  `json:"range_comments,string"`
	RangeLikes    int64  `json:"range_likes,string"`
	Posts         int64  `json:"posts,string"`
	PostLikes     int64  `json:"post_likes,string"`
	Comments      int64  `json:"comments,string"`
	Likes         int64  `json:"likes,string"`
	Shares        int64  `json:"shares,string"`
}
type adminTicketStatistics struct {
	Remaining        string `json:"remaining"`
	Total            string `json:"total"`
	QuotaPerUnit     string `json:"quota_per_unit"`
	Spent            string `json:"spent"`
	Removed          string `json:"removed"`
	RewardQuota      string `json:"reward_quota"`
	RewardExperience string `json:"reward_experience"`
}

func (g *mysqlBindingGuard) adminCommunityStatistics(ctx context.Context, ids []string) (map[string]*adminCommunityStatistics, error) {
	return g.adminCommunityStatisticsAt(ctx, ids, time.Now())
}
func beijingStatisticsStart(now time.Time) time.Time {
	local := now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}
func (g *mysqlBindingGuard) adminCommunityStatisticsAt(ctx context.Context, ids []string, now time.Time) (map[string]*adminCommunityStatistics, error) {
	rangeStart := beijingStatisticsStart(now)
	return g.adminCommunityStatisticsRange(ctx, ids, rangeStart, rangeStart.AddDate(0, 0, 1), now)
}
func parseStatisticsRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	if from == "" && to == "" {
		start := beijingStatisticsStart(now)
		return start, start.AddDate(0, 0, 1), nil
	}
	start, e1 := time.ParseInLocation("2006-01-02", from, zone)
	end, e2 := time.ParseInLocation("2006-01-02", to, zone)
	if e1 != nil || e2 != nil || start.Format("2006-01-02") != from || end.Format("2006-01-02") != to || end.Before(start) || end.After(beijingStatisticsStart(now)) {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid statistics dates")
	}
	return start, end.AddDate(0, 0, 1), nil
}
func (g *mysqlBindingGuard) adminCommunityStatisticsRange(ctx context.Context, ids []string, from, to, now time.Time) (map[string]*adminCommunityStatistics, error) {
	result := make(map[string]*adminCommunityStatistics, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
		result[id] = &adminCommunityStatistics{RangeStart: from.Format("2006-01-02"), RangeEnd: to.AddDate(0, 0, -1).Format("2006-01-02")}
	}
	in := "(?" + strings.Repeat(",?", len(ids)-1) + ")"
	queries := []string{
		"SELECT user_id,COUNT(*) FROM question WHERE status IN(1,2) AND user_id IN " + in + " GROUP BY user_id",
		"SELECT q.user_id,COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type JOIN question q ON q.id=a.original_object_id WHERE c.`key`='question.voted_up' AND a.cancelled=0 AND q.status IN(1,2) AND q.user_id IN " + in + " GROUP BY q.user_id",
		"SELECT user_id,SUM(n) FROM (SELECT user_id,COUNT(*) n FROM answer WHERE status=1 AND user_id IN " + in + " GROUP BY user_id UNION ALL SELECT user_id,COUNT(*) n FROM comment WHERE status=1 AND user_id IN " + in + " GROUP BY user_id) t GROUP BY user_id",
		"SELECT a.trigger_user_id,COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type WHERE a.cancelled=0 AND c.`key` IN ('question.voted_up','answer.voted_up') AND a.trigger_user_id IN " + in + " GROUP BY a.trigger_user_id",
		"SELECT q.user_id,SUM(s.clicks) FROM metar_question_share s JOIN question q ON q.id=s.question_id WHERE q.status IN(1,2) AND q.user_id IN " + in + " GROUP BY q.user_id",
	}
	for kind, query := range queries {
		params := args
		if kind == 2 {
			params = append(append([]any{}, args...), args...)
		}
		rows, err := g.db.QueryContext(ctx, query, params...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var n int64
			if err = rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			stat := result[id]
			if stat == nil {
				continue
			}
			switch kind {
			case 0:
				stat.Posts = n
			case 1:
				stat.PostLikes = n
			case 2:
				stat.Comments = n
			case 3:
				stat.Likes = n
			case 4:
				stat.Shares = n
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	start, end := from.Unix(), to.Unix()
	if end > now.Unix()+1 {
		end = now.Unix() + 1
	}
	// Count answers and comments separately before summing: both may have a row
	// for the same author (and may even have the same count).
	responseFilter := " WHERE status=1 AND created_at>=FROM_UNIXTIME(?) AND created_at<FROM_UNIXTIME(?) AND user_id IN " + in + " GROUP BY user_id"
	rangeQueries := []string{
		"SELECT user_id,SUM(n) FROM (SELECT user_id,COUNT(*) n FROM answer" + responseFilter +
			" UNION ALL SELECT user_id,COUNT(*) n FROM comment" + responseFilter + ") t GROUP BY user_id",
		"SELECT a.trigger_user_id,COUNT(*) FROM activity a JOIN config c ON c.id=a.activity_type WHERE a.cancelled=0 AND c.`key` IN ('question.voted_up','answer.voted_up','comment.voted_up') AND a.created_at>=FROM_UNIXTIME(?) AND a.created_at<FROM_UNIXTIME(?) AND a.trigger_user_id IN " + in + " GROUP BY a.trigger_user_id",
	}
	for kind, query := range rangeQueries {
		params := append([]any{start, end}, args...)
		if kind == 0 {
			params = append(params, params...)
		}
		rows, err := g.db.QueryContext(ctx, query, params...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var n int64
			if err = rows.Scan(&id, &n); err != nil {
				rows.Close()
				return nil, err
			}
			if stat := result[id]; stat != nil {
				if kind == 0 {
					stat.RangeComments = n
				} else {
					stat.RangeLikes = n
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

var shareStatisticsLimiter imageLimiter

func (uc *UserCenter) registerShareStatistics(r *gin.RouterGroup, siteURL func() string) {
	r.POST("/metar/statistics/share", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		if !ticketGrantEnabled() || c.Request.URL.RawQuery != "" || c.GetHeader("X-Metar-Request") != "1" || !communitySameOrigin(c.Request, siteURL()) {
			c.Status(http.StatusForbidden)
			return
		}
		var body struct {
			QuestionID string `json:"question_id"`
		}
		if !experienceJSON(c, &body) || len(body.QuestionID) > 24 {
			c.Status(400)
			return
		}
		originalID := body.QuestionID
		body.QuestionID = uid.DeShortID(originalID)
		if !canonicalCommunityID(body.QuestionID) || (originalID != body.QuestionID && uid.EnShortID(body.QuestionID) != originalID) {
			c.Status(400)
			return
		}
		release, allowed := shareStatisticsLimiter.acquire(c.RemoteIP(), time.Now())
		if !allowed {
			c.Status(429)
			return
		}
		defer release()
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		uc.runtimeMu.RLock()
		defer uc.runtimeMu.RUnlock()
		guard, ok := uc.Guard.(*mysqlBindingGuard)
		if !ok {
			c.Status(503)
			return
		}
		id, _ := strconv.ParseInt(body.QuestionID, 10, 64)
		_, err := guard.db.ExecContext(ctx, `INSERT INTO metar_question_share(question_id,clicks) SELECT id,1 FROM question WHERE id=? AND status IN(1,2) AND `+"`show`"+`=1 ON DUPLICATE KEY UPDATE clicks=clicks+1`, id)
		if err != nil {
			c.Status(503)
			return
		}
		c.Status(204)
	})
}
