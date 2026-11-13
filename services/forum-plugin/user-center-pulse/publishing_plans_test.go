package pulse_user_center

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublishingPlanValidationAndThreadResolution(t *testing.T) {
	q := validPublishingItem()
	a := q
	a.Kind = "answer"
	a.Title = ""
	a.Tags = nil
	c := a
	c.Kind = "comment"
	p := publishingPlan{Key: "schedule-123456789", Items: []publishingPlanItem{{publishingItem: q, ID: "topic"}, {publishingItem: a, ID: "reply", Parent: "topic", PublishAt: "2026-11-14T10:00:00+08:00"}, {publishingItem: c, ID: "comment", Parent: "reply"}, {publishingItem: c, ID: "nested", Parent: "comment"}}}
	if err := validatePublishingPlan(p); err != nil {
		t.Fatal(err)
	}
	results := map[string]publishingPlanResult{"topic": {Status: "published", ObjectID: "101"}, "reply": {Status: "published", ObjectID: "102", QuestionID: "101"}, "comment": {Status: "published", ObjectID: "103", QuestionID: "101"}}
	item, ok := resolvePlanItem(p, p.Items[3], results)
	if !ok || item.ObjectID != "102" || item.ReplyCommentID != "103" || item.QuestionID != "101" {
		t.Fatal(item, ok)
	}
	var payload map[string]string
	json.Unmarshal(publishingPayload(item, "metar-ops-7-key"), &payload)
	if payload["object_id"] != "102" || payload["reply_comment_id"] != "103" || !strings.Contains(payload["original_text"], "运营代发") {
		t.Fatal(payload)
	}
	delete(results, "comment")
	if _, ok := resolvePlanItem(p, p.Items[3], results); ok {
		t.Fatal("unresolved parent accepted")
	}
	for _, mutate := range []func(*publishingPlan){func(p *publishingPlan) { p.Items[1].Parent = "nested" }, func(p *publishingPlan) { p.Items[1].PublishAt = "2026-11-14T10:00:00" }, func(p *publishingPlan) { p.Items[1].ID = "topic" }, func(p *publishingPlan) { p.Items[3].Content = strings.Repeat("字", 401) }, func(p *publishingPlan) { p.Items[1].QuestionID = "100" }} {
		copyPlan := p
		copyPlan.Items = append([]publishingPlanItem(nil), p.Items...)
		mutate(&copyPlan)
		if validatePublishingPlan(copyPlan) == nil {
			t.Fatal("accepted invalid plan")
		}
	}
}
