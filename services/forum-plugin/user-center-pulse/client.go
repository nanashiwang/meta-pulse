package pulse_user_center

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	pulseHeaderUserID    = "X-Pulse-User-Id"
	pulseHeaderRole      = "X-Pulse-Role"
	pulseHeaderTimestamp = "X-Pulse-Timestamp"
	pulseHeaderNonce     = "X-Pulse-Nonce"
	pulseHeaderSignature = "X-Pulse-Signature"
	forumServiceRole     = "forum"
)

// UserProfile is the read-only projection Meta Pulse exposes to the forum.
//
// Meta Pulse remains the source of truth for levels and contribution; the forum
// never writes back. See docs/COMMUNITY.md for why this direction is enforced.
type UserProfile struct {
	UserID                 int64  `json:"user_id"`
	Level                  int    `json:"level"`
	LevelName              string `json:"level_name"`
	LifetimeContributionMi int64  `json:"lifetime_contribution_milli"`
	Suspended              bool   `json:"suspended"`
}

// PulseClient talks to meta-pulse-api's internal read-only endpoint.
type PulseClient struct {
	config *Config
	http   *http.Client
	now    func() time.Time
	nonce  func() (string, error)
}

func NewPulseClient(config *Config) *PulseClient {
	return &PulseClient{
		config: config,
		http:   &http.Client{Timeout: 3 * time.Second},
		now:    time.Now,
		nonce:  pulseRequestNonce,
	}
}

// GetUserProfile fetches a single user's Pulse profile.
//
// Pulse being unavailable must never block forum login, so callers treat an
// error here as "no badge" rather than a failure.
func (c *PulseClient) GetUserProfile(externalID string) (*UserProfile, error) {
	if c == nil || c.config == nil {
		return nil, fmt.Errorf("pulse client not configured")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.config.PulseBaseURL), "/")
	parsedBase, err := url.Parse(baseURL)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return nil, fmt.Errorf("pulse base url not configured")
	}
	userID, err := strconv.ParseUint(strings.TrimSpace(externalID), 10, 64)
	if err != nil || userID == 0 {
		return nil, fmt.Errorf("invalid external user id")
	}

	target := fmt.Sprintf("%s/v1/internal/users/%s/profile", baseURL, url.PathEscape(externalID))
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if err := c.sign(req, externalID); err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pulse profile request failed: %d", resp.StatusCode)
	}

	profile := &UserProfile{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(profile); err != nil {
		return nil, err
	}
	if profile.UserID != int64(userID) {
		return nil, fmt.Errorf("pulse profile identity mismatch")
	}
	return profile, nil
}

// sign applies the canonical Meta Pulse HMAC contract. The forum authenticates
// as a service; it never forwards browser cookies or trusts browser headers.
func (c *PulseClient) sign(req *http.Request, userID string) error {
	if c == nil || c.config == nil || strings.TrimSpace(c.config.PulseHMACSecret) == "" {
		return fmt.Errorf("pulse hmac secret not configured")
	}
	if req == nil || req.URL == nil || c.now == nil || c.nonce == nil {
		return fmt.Errorf("pulse request signer not configured")
	}
	nonce, err := c.nonce()
	if err != nil {
		return fmt.Errorf("generate pulse request nonce: %w", err)
	}
	timestamp := c.now().Unix()
	canonical := pulseCanonicalPayload(req.Method, req.URL.EscapedPath(), userID, forumServiceRole, timestamp, nonce, nil)
	mac := hmac.New(sha256.New, []byte(c.config.PulseHMACSecret))
	_, _ = mac.Write([]byte(canonical))

	req.Header.Set(pulseHeaderUserID, userID)
	req.Header.Set(pulseHeaderRole, forumServiceRole)
	req.Header.Set(pulseHeaderTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(pulseHeaderNonce, nonce)
	req.Header.Set(pulseHeaderSignature, hex.EncodeToString(mac.Sum(nil)))
	return nil
}

func pulseCanonicalPayload(method, path, userID, role string, timestamp int64, nonce string, body []byte) string {
	digest := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(method)), path, userID, role,
		strconv.FormatInt(timestamp, 10), nonce, hex.EncodeToString(digest[:]),
	}, "\n")
}

func pulseRequestNonce() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
