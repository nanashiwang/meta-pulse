package newapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/security"
)

func TestBenefitClientSignsGrantAndDecodesResponse(t *testing.T) {
	secret := []byte("benefit-secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/pulse/benefits/grant" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(security.HeaderRole) != "pulse-settlement" || r.Header.Get(security.HeaderSignature) == "" || r.Header.Get(security.HeaderNonce) == "" {
			t.Fatalf("missing signed headers: %+v", r.Header)
		}
		var request ports.BenefitGrantRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.GrantID != "pg_1" || request.SourceRef != "pg_1" || request.PayloadHash != strings.Repeat("a", 64) {
			t.Fatalf("request=%+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"applied","source_ref":"pg_1"}`))
	}))
	defer server.Close()
	client, err := NewBenefitClient(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Grant(context.Background(), ports.BenefitGrantRequest{GrantID: "pg_1", UserID: 9, Amount: 10, SourceRef: "pg_1", RewardType: "quota", PayloadHash: strings.Repeat("a", 64)})
	if err != nil || !result.Applied || result.SourceRef != "pg_1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBenefitClientMapsConflictAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/internal/pulse/benefits/grant":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"different payload"}`))
		case "/api/internal/pulse/benefits/query":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	client, err := NewBenefitClient(server.URL, []byte("benefit-secret"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Grant(context.Background(), ports.BenefitGrantRequest{GrantID: "pg_1", UserID: 9, Amount: 10, SourceRef: "pg_1", RewardType: "quota", PayloadHash: strings.Repeat("a", 64)})
	if !errors.Is(err, ErrBenefitConflict) {
		t.Fatalf("grant err=%v", err)
	}
	state, err := client.Query(context.Background(), "pg_1")
	if err != nil || state.Applied || state.SourceRef != "pg_1" {
		t.Fatalf("query state=%+v err=%v", state, err)
	}
}

func TestBenefitClientRejectsTransferableQuota(t *testing.T) {
	client, err := NewBenefitClient("http://example.com", []byte("secret"), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Grant(context.Background(), ports.BenefitGrantRequest{GrantID: "x", UserID: 1, Amount: 1, SourceRef: "x", TransferableQuota: true, PayloadHash: strings.Repeat("a", 64)})
	if err == nil || !strings.Contains(err.Error(), "invalid benefit grant") {
		t.Fatalf("err=%v", err)
	}
}

func TestBenefitClientKeepsRolledBackSeparateFromApplied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// applied=true is the legacy new-api response. The explicit status must
		// remain authoritative so a reversed reward can never look active.
		_, _ = w.Write([]byte(`{"status":"rolled_back","applied":true,"source_ref":"pg_1"}`))
	}))
	defer server.Close()
	client, err := NewBenefitClient(server.URL, []byte("benefit-secret"), server.Client())
	if err != nil {
		t.Fatal(err)
	}

	state, err := client.Query(context.Background(), "pg_1")
	if err != nil || state.Applied || !state.RolledBack || state.Status != ports.BenefitStatusRolledBack {
		t.Fatalf("query state=%+v err=%v", state, err)
	}
	rollback, err := client.Rollback(context.Background(), "pg_1", "fraud")
	if err != nil || rollback.Applied || !rollback.RolledBack {
		t.Fatalf("rollback state=%+v err=%v", rollback, err)
	}
	grant, err := client.Grant(context.Background(), ports.BenefitGrantRequest{GrantID: "pg_1", UserID: 9, Amount: 10, SourceRef: "pg_1", RewardType: "quota", PayloadHash: strings.Repeat("a", 64)})
	if err != nil || grant.Applied || !grant.RolledBack {
		t.Fatalf("grant state=%+v err=%v", grant, err)
	}
}

func TestBenefitClientRejectsUnknownState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"unknown","source_ref":"pg_1"}`))
	}))
	defer server.Close()
	client, err := NewBenefitClient(server.URL, []byte("benefit-secret"), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Query(context.Background(), "pg_1")
	if err == nil || !strings.Contains(err.Error(), "unsupported status") {
		t.Fatalf("err=%v", err)
	}
}
