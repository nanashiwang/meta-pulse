package newapi

import (
	"context"
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
		if r.URL.Path != "/internal/pulse/benefits/grant" || r.Method != http.MethodPost {
			t.Fatalf("request=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get(security.HeaderRole) != "pulse-settlement" || r.Header.Get(security.HeaderSignature) == "" || r.Header.Get(security.HeaderNonce) == "" {
			t.Fatalf("missing signed headers: %+v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"applied","source_ref":"pg_1"}`))
	}))
	defer server.Close()
	client, err := NewBenefitClient(server.URL, secret, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Grant(context.Background(), ports.BenefitGrantRequest{UserID: 9, Amount: 10, SourceRef: "pg_1", RewardType: "quota"})
	if err != nil || !result.Applied || result.SourceRef != "pg_1" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBenefitClientMapsConflictAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/pulse/benefits/grant":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"different payload"}`))
		case "/internal/pulse/benefits/query":
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
	_, err = client.Grant(context.Background(), ports.BenefitGrantRequest{UserID: 9, Amount: 10, SourceRef: "pg_1", RewardType: "quota"})
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
	_, err = client.Grant(context.Background(), ports.BenefitGrantRequest{UserID: 1, Amount: 1, SourceRef: "x", TransferableQuota: true})
	if err == nil || !strings.Contains(err.Error(), "invalid benefit grant") {
		t.Fatalf("err=%v", err)
	}
}
