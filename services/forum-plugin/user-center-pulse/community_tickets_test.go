package pulse_user_center

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCommunityTasksRejectBrowserIdentityAndUnbound(t *testing.T) {
	for _, op := range []string{"tasks?user_id=1", "tasks?count=100", "tasks?Authorization=secret"} {
		r, _, guard := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Fatal("tasks proxied to Pulse") })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, communityTestRequest("GET", op, ""))
		if w.Code != 400 || guard.seen != "" {
			t.Fatal("invalid query", op, w.Code)
		}
	}
	for _, err := range []error{errCommunityUnauthenticated, errCommunityAccountUnavailable, errCommunityBindingRequired} {
		r, _, guard := communityTestRouter(t, func(http.ResponseWriter, *http.Request) { t.Fatal("tasks proxied to Pulse") })
		guard.err = err
		w := httptest.NewRecorder()
		r.ServeHTTP(w, communityTestRequest("GET", "tasks", ""))
		if w.Code != 401 && w.Code != 403 {
			t.Fatal("invalid identity", w.Code)
		}
	}
}
