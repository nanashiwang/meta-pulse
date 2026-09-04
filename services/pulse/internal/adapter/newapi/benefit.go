package newapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nanashiwang/meta-pulse/internal/ports"
	"github.com/nanashiwang/meta-pulse/internal/security"
)

var (
	ErrBenefitConflict = ports.ErrBenefitPayloadConflict
	ErrBenefitNotFound = errors.New("new-api benefit not found")
)

const maxBenefitResponseBytes = 1 << 20

type BenefitClient struct {
	baseURL string
	secret  []byte
	role    string
	http    *http.Client
	now     func() time.Time
}

func NewBenefitClient(baseURL string, secret []byte, client *http.Client) (*BenefitClient, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || (parsed.RawPath != "" && parsed.RawPath != "/") {
		return nil, errors.New("new-api benefit base URL is invalid")
	}
	if len(secret) == 0 {
		return nil, errors.New("new-api benefit service secret is empty")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return &BenefitClient{baseURL: parsed.String(), secret: append([]byte(nil), secret...), role: "pulse-settlement", http: client, now: time.Now}, nil
}

func (c *BenefitClient) Grant(ctx context.Context, request ports.BenefitGrantRequest) (ports.BenefitGrantResponse, error) {
	if request.GrantID == "" || request.GrantID != request.SourceRef || request.UserID == 0 || request.Amount <= 0 || request.TransferableQuota || len(request.PayloadHash) != sha256.Size*2 {
		return ports.BenefitGrantResponse{}, errors.New("invalid benefit grant request")
	}
	var response benefitResponse
	if err := c.post(ctx, "/api/internal/pulse/benefits/grant", request.UserID, request, &response); err != nil {
		return ports.BenefitGrantResponse{}, err
	}
	state, err := response.state()
	if err != nil {
		return ports.BenefitGrantResponse{}, err
	}
	return ports.BenefitGrantResponse{Applied: state.Applied, RolledBack: state.RolledBack, Status: state.Status, SourceRef: state.SourceRef}, nil
}

func (c *BenefitClient) Query(ctx context.Context, sourceRef string) (ports.BenefitState, error) {
	if strings.TrimSpace(sourceRef) == "" {
		return ports.BenefitState{}, errors.New("benefit source ref is empty")
	}
	var response benefitResponse
	if err := c.post(ctx, "/api/internal/pulse/benefits/query", 1, map[string]string{"source_ref": sourceRef}, &response); err != nil {
		if errors.Is(err, ErrBenefitNotFound) {
			return ports.BenefitState{Status: ports.BenefitStatusNotFound, SourceRef: sourceRef}, nil
		}
		return ports.BenefitState{}, err
	}
	return response.state()
}

func (c *BenefitClient) Rollback(ctx context.Context, sourceRef, reason string) (ports.BenefitState, error) {
	if strings.TrimSpace(sourceRef) == "" || strings.TrimSpace(reason) == "" {
		return ports.BenefitState{}, errors.New("benefit rollback request is incomplete")
	}
	var response benefitResponse
	if err := c.post(ctx, "/api/internal/pulse/benefits/rollback", 1, map[string]string{"source_ref": sourceRef, "reason": reason}, &response); err != nil {
		return ports.BenefitState{}, err
	}
	return response.state()
}

type benefitResponse struct {
	Applied    bool   `json:"applied"`
	RolledBack bool   `json:"rolled_back"`
	Status     string `json:"status"`
	SourceRef  string `json:"source_ref"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (r benefitResponse) state() (ports.BenefitState, error) {
	state := ports.BenefitState{Status: strings.TrimSpace(r.Status), SourceRef: r.SourceRef}
	switch state.Status {
	case ports.BenefitStatusApplied, ports.BenefitStatusAlreadyApplied:
		if r.RolledBack {
			return ports.BenefitState{}, errors.New("new-api benefit response has contradictory state")
		}
		state.Applied = true
	case ports.BenefitStatusRolledBack:
		// status is authoritative during a rolling upgrade: older new-api
		// versions returned applied=true even after a reversal.
		state.RolledBack = true
	default:
		return ports.BenefitState{}, fmt.Errorf("new-api benefit response has unsupported status %q", state.Status)
	}
	return state, nil
}

func (c *BenefitClient) post(ctx context.Context, path string, userID uint64, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal benefit request: %w", err)
	}
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return fmt.Errorf("generate benefit nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	timestamp := c.now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create benefit request: %w", err)
	}
	user := fmt.Sprintf("%d", userID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(security.HeaderUserID, user)
	req.Header.Set(security.HeaderRole, c.role)
	req.Header.Set(security.HeaderTimestamp, fmt.Sprintf("%d", timestamp))
	req.Header.Set(security.HeaderNonce, nonce)
	canonical := security.CanonicalPayload(http.MethodPost, path, user, c.role, timestamp, nonce, body)
	req.Header.Set(security.HeaderSignature, signHex(c.secret, canonical))
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call new-api benefit API: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxBenefitResponseBytes+1))
	if readErr != nil {
		return fmt.Errorf("read new-api benefit response: %w", readErr)
	}
	if len(responseBody) > maxBenefitResponseBytes {
		return fmt.Errorf("new-api benefit response exceeds %d bytes", maxBenefitResponseBytes)
	}
	var envelope benefitResponse
	if len(responseBody) > 0 {
		_ = json.Unmarshal(responseBody, &envelope)
	}
	if response.StatusCode == http.StatusConflict {
		return fmt.Errorf("%w: %s", ErrBenefitConflict, envelope.Message)
	}
	if response.StatusCode == http.StatusNotFound {
		return ErrBenefitNotFound
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("new-api benefit API returned status %d: %s", response.StatusCode, envelope.Message)
	}
	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			return fmt.Errorf("decode new-api benefit response: %w", err)
		}
	}
	return nil
}

func signHex(secret []byte, canonical string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}
