package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
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
}

func NewPulseClient(config *Config) *PulseClient {
	return &PulseClient{
		config: config,
		http:   &http.Client{Timeout: 3 * time.Second},
	}
}

// GetUserProfile fetches a single user's Pulse profile.
//
// Pulse being unavailable must never block forum login, so callers treat an
// error here as "no badge" rather than a failure.
func (c *PulseClient) GetUserProfile(externalID string) (*UserProfile, error) {
	if c.config.PulseBaseURL == "" {
		return nil, fmt.Errorf("pulse base url not configured")
	}

	url := fmt.Sprintf("%s/v1/internal/users/%s/profile", c.config.PulseBaseURL, externalID)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.sign(req, externalID)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pulse profile request failed: %d", resp.StatusCode)
	}

	profile := &UserProfile{}
	if err := json.NewDecoder(resp.Body).Decode(profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// sign applies the same HMAC scheme Meta Pulse uses for its signed BFF headers,
// so the forum authenticates as a service rather than as a browser.
func (c *PulseClient) sign(req *http.Request, userID string) {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	payload := userID + "\n" + timestamp

	mac := hmac.New(sha256.New, []byte(c.config.HMACSecret))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("X-Pulse-User-Id", userID)
	req.Header.Set("X-Pulse-Timestamp", timestamp)
	req.Header.Set("X-Pulse-Signature", signature)
}
