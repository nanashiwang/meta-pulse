package pulse_user_center

import (
	"context"
	"encoding/json"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/communitytickets"
	"net/http"
	"testing"
)

func TestFlashDeliveryRevalidatesBindingBeforeSendingSavedAmount(t *testing.T) {
	for _, identity := range []string{"99", ""} {
		_, uc, g := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Fatal("changed binding contacted upstream") })
		g.identity = identity
		err := uc.deliverCheckinFlash(context.Background(), nil, communitytickets.FlashReceipt{UserID: "7", ExternalID: "42", Day: "2026-11-18", Cents: 30, EventID: 1}, true)
		if err == nil {
			t.Fatal("changed binding accepted")
		}
	}
}
func TestFlashDeliverySubmitsOnlyPersistedServerFieldsAndQueriesOriginalGrant(t *testing.T) {
	for _, grant := range []string{"", "pg_existing"} {
		calls := 0
		_, uc, _ := communityTestRouter(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Header.Get(pulseHeaderUserID) != "42" || r.Header.Get(pulseHeaderRole) != "community-bff" || r.Header.Get(pulseHeaderSignature) == "" || r.Header.Get("Authorization") != "" {
				t.Error("wrong trusted identity")
			}
			if grant == "" {
				if r.Method != "POST" || r.URL.Path != "/v1/internal/me/checkin-flash" {
					t.Error(r.Method, r.URL.Path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 3 || body["cents"] != float64(30) || body["event_id"] != float64(1) || body["day"] != "2026-11-18" {
					t.Error(body, err)
				}
			} else {
				if r.Method != "GET" || r.URL.Path != "/v1/internal/me/checkin-flash/pg_existing" {
					t.Error(r.Method, r.URL.Path)
				}
			}
			w.WriteHeader(503)
		})
		err := uc.deliverCheckinFlash(context.Background(), nil, communitytickets.FlashReceipt{UserID: "7", ExternalID: "42", Day: "2026-11-18", Cents: 30, EventID: 1, GrantID: grant}, true)
		if err == nil || calls != 1 {
			t.Fatal("failure lost", err, calls)
		}
	}
}
