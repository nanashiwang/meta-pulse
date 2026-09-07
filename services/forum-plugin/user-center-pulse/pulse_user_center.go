package pulse_user_center

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/answer-plugins/util"
	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/i18n"
	"github.com/segmentfault/pacman/log"
)

const (
	forumLoginFlowCookie    = "meta_pulse_forum_flow"
	forumCallbackPath       = "/api/user-center/login/callback"
	forumConnectorLoginPath = "/answer/api/v1/connector/login/pulse_user_center"
	forumSSOBootstrapPath   = "/api/forum/sso/bootstrap"
	forumLocalSignUpPath    = "/users/register"
	loginFlowTTL            = 10 * time.Minute
	maxLoginQueryBytes      = 8 << 10
)

var loginTicketFields = []string{
	"user_id", "username", "display_name", "email", "avatar", "timestamp", "nonce", "signature",
}

//go:embed info.yaml
var Info embed.FS

// UserCenter is both an Answer Connector and a non-authoritative UserCenter.
// Answer owns local registration, passwords, sessions, profile and moderation;
// the Connector only binds one immutable new-api identity, while UserCenter
// contributes optional Pulse branding for bound accounts.
type UserCenter struct {
	runtimeMu sync.RWMutex
	Config    *Config
	Client    *PulseClient
	Logins    LoginFlowStore
	Guard     BindingGuard

	newBindingGuard func(string) (BindingGuard, error)
}

func (uc *UserCenter) configSnapshot() Config {
	if uc == nil {
		return Config{}
	}
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	if uc.Config == nil {
		return Config{}
	}
	return *uc.Config
}

func init() {
	plugin.Register(&UserCenter{Config: &Config{}})
}

func (uc *UserCenter) Info() plugin.Info {
	info := &util.Info{}
	info.GetInfo(Info)

	return plugin.Info{
		Name:        plugin.MakeTranslator(i18n.InfoName),
		SlugName:    info.SlugName,
		Description: plugin.MakeTranslator(i18n.InfoDescription),
		Author:      info.Author,
		Version:     info.Version,
		Link:        info.Link,
	}
}

func (uc *UserCenter) Description() plugin.UserCenterDesc {
	config := uc.configSnapshot()
	return plugin.UserCenterDesc{
		Name:        "Meta Pulse",
		DisplayName: plugin.MakeTranslator(i18n.InfoName),
		Icon:        "",
		Url:         config.NewAPIBaseURL,

		// Answer v1.7.1 always exposes its UserCenter redirect endpoints when a
		// UserCenter plugin is enabled. Leaving these empty makes the login button
		// redirect to a non-existent parent path. Keep the optional external login
		// on the hardened Connector flow and keep registration inside Answer.
		LoginRedirectURL:  forumConnectorLoginPath,
		SignUpRedirectURL: forumLocalSignUpPath,

		RankAgentEnabled:          false,
		UserStatusAgentEnabled:    false,
		UserRoleAgentEnabled:      false,
		MustAuthEmailEnabled:      false,
		EnabledOriginalUserSystem: true,
	}
}

func (uc *UserCenter) ControlCenterItems() []plugin.ControlCenter {
	config := uc.configSnapshot()
	return []plugin.ControlCenter{
		{Name: "Meta Pulse", Label: "Meta Pulse", Url: config.NewAPIBaseURL + "/console/pulse"},
		{Name: "Console", Label: "API Console", Url: config.NewAPIBaseURL + "/console"},
	}
}

func (uc *UserCenter) ConnectorLogoSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none"><path d="M3 12h4l2-6 4 12 2-6h6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

func (uc *UserCenter) ConnectorName() plugin.Translator {
	return plugin.MakeTranslator(i18n.ConnectorName)
}

func (uc *UserCenter) ConnectorSlugName() string { return pluginSlug }

// ConnectorSender starts a browser-bound flow before leaving the forum. The
// marker cannot replace an OAuth state (new-api's existing ticket has no state
// field), but it prevents an unsolicited callback URL from being accepted and
// is consumed atomically with the signed ticket nonce.
//
// The first hop is a same-origin bootstrap page on new-api. Its session cookie
// is currently SameSite=Strict, so a direct cross-site navigation from metar.uk
// would omit an otherwise valid new-api session. The bootstrap commits a
// new-api document first, then navigates to /api/forum/sso/start same-origin.
func (uc *UserCenter) ConnectorSender(ctx *plugin.GinContext, _ string) string {
	if ctx == nil || ctx.Request == nil || uc == nil {
		return "/50x"
	}
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	if uc.Config == nil || uc.Logins == nil {
		return "/50x"
	}
	flowID, err := randomHex(32)
	if err != nil {
		log.Errorf("create forum login flow: %v", err)
		return "/50x"
	}
	expiresAt := time.Now().Add(loginFlowTTL)
	if err := uc.Logins.Begin(ctx.Request.Context(), flowID, expiresAt); err != nil {
		log.Errorf("store forum login flow: %v", err)
		return "/50x"
	}
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     forumLoginFlowCookie,
		Value:    flowID,
		Path:     forumCallbackPath,
		MaxAge:   int(loginFlowTTL.Seconds()),
		Expires:  expiresAt,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	ctx.Header("Cache-Control", "no-store")
	ctx.Header("Referrer-Policy", "no-referrer")
	return uc.Config.NewAPIBaseURL + forumSSOBootstrapPath
}

// ConnectorReceiver verifies the fixed new-api Login Ticket, requires a
// browser-initiated flow, and returns only the stable external identity. Email
// and avatar deliberately remain empty: new-api does not prove historical
// email verification, and the local Answer profile belongs to the community.
func (uc *UserCenter) ConnectorReceiver(ctx *plugin.GinContext, _ string) (plugin.ExternalLoginUserInfo, error) {
	var empty plugin.ExternalLoginUserInfo
	if ctx == nil || ctx.Request == nil || uc == nil {
		return empty, errors.New("forum connector is not configured")
	}
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	if uc.Config == nil || uc.Guard == nil || uc.Logins == nil {
		return empty, errors.New("forum connector is not configured")
	}
	flowID, err := ctx.Cookie(forumLoginFlowCookie)
	if err != nil || !validLoginFlowID(flowID) {
		return empty, errors.New("forum login flow is missing or expired")
	}
	ticket, err := loginTicketFromRequest(ctx.Request)
	if err != nil {
		return empty, errors.New("login verification failed")
	}
	secrets := []string{uc.Config.SSOHMACSecret}
	if previous := uc.Config.SSOHMACSecretPrevious; previous != "" && previous != uc.Config.SSOHMACSecret {
		secrets = append(secrets, previous)
	}
	expiresAt, err := ticket.AuthenticateWithSecrets(secrets, time.Now())
	if err != nil {
		log.Warnf("rejected forum connector callback: %v", err)
		return empty, errors.New("login verification failed")
	}
	// Expensive schema checks happen only after the callback proves both a
	// browser-started flow and a valid new-api signature. Guard failure still
	// occurs before the flow or ticket nonce is consumed.
	if err := uc.Guard.Ready(ctx.Request.Context()); err != nil {
		log.Errorf("forum binding guard unavailable: %v", err)
		return empty, errors.New("forum account binding is temporarily unavailable")
	}
	accepted, err := uc.Logins.Consume(ctx.Request.Context(), flowID, ticket.Nonce, expiresAt)
	if err != nil {
		log.Errorf("consume forum login flow: %v", err)
		return empty, errors.New("login verification temporarily unavailable")
	}
	if !accepted {
		clearLoginFlowCookie(ctx)
		return empty, errors.New("forum login flow was already used or expired")
	}
	clearLoginFlowCookie(ctx)
	displayName := ticket.DisplayName
	if displayName == "" {
		displayName = ticket.Username
	}
	return plugin.ExternalLoginUserInfo{
		ExternalID:  ticket.UserID,
		Username:    ticket.Username,
		DisplayName: displayName,
		Email:       "",
		Avatar:      "",
		MetaInfo:    "",
	}, nil
}

func validLoginFlowID(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value && !strings.ContainsAny(value, "\r\n")
}

func clearLoginFlowCookie(ctx *gin.Context) {
	http.SetCookie(ctx.Writer, &http.Cookie{
		Name:     forumLoginFlowCookie,
		Value:    "",
		Path:     forumCallbackPath,
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func loginTicketFromRequest(request *http.Request) (*LoginTicket, error) {
	if request == nil || request.URL == nil || request.Method != http.MethodGet || len(request.URL.RawQuery) > maxLoginQueryBytes {
		return nil, errors.New("invalid login callback request")
	}
	query, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil || len(query) != len(loginTicketFields) {
		return nil, errors.New("invalid login callback query")
	}
	for _, field := range loginTicketFields {
		if len(query[field]) != 1 {
			return nil, fmt.Errorf("login callback field %s must occur exactly once", field)
		}
	}
	rawTimestamp := query.Get("timestamp")
	timestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil || timestamp <= 0 || strconv.FormatInt(timestamp, 10) != rawTimestamp {
		return nil, errors.New("invalid timestamp in login ticket")
	}
	return &LoginTicket{
		UserID:      query.Get("user_id"),
		Username:    query.Get("username"),
		DisplayName: query.Get("display_name"),
		Email:       query.Get("email"),
		Avatar:      query.Get("avatar"),
		Timestamp:   timestamp,
		Nonce:       query.Get("nonce"),
		Signature:   query.Get("signature"),
	}, nil
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("invalid random value size")
	}
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// The old UserCenter SSO callbacks are disabled. The edge rewrites the fixed
// new-api callback to the Connector receiver so users can keep local accounts.
func (uc *UserCenter) LoginCallback(*plugin.GinContext) (*plugin.UserCenterBasicUserInfo, error) {
	return nil, errors.New("user-center login is disabled; use the Meta API connector")
}

func (uc *UserCenter) SignUpCallback(*plugin.GinContext) (*plugin.UserCenterBasicUserInfo, error) {
	return nil, errors.New("user-center sign-up is disabled; use local registration")
}

func (uc *UserCenter) UserInfo(externalID string) (*plugin.UserCenterBasicUserInfo, error) {
	return &plugin.UserCenterBasicUserInfo{ExternalID: externalID, Status: plugin.UserStatusAvailable}, nil
}

// Local Answer status remains authoritative; Description disables this agent.
func (uc *UserCenter) UserStatus(string) plugin.UserStatus { return plugin.UserStatusAvailable }

func (uc *UserCenter) UserList(externalIDs []string) ([]*plugin.UserCenterBasicUserInfo, error) {
	users := make([]*plugin.UserCenterBasicUserInfo, 0, len(externalIDs))
	for _, externalID := range externalIDs {
		users = append(users, &plugin.UserCenterBasicUserInfo{ExternalID: externalID, Status: plugin.UserStatusAvailable})
	}
	return users, nil
}

// Empty redirects preserve Answer's local profile/password settings.
func (uc *UserCenter) UserSettings(string) (*plugin.SettingInfo, error) {
	return &plugin.SettingInfo{}, nil
}

func (uc *UserCenter) PersonalBranding(externalID string) []*plugin.PersonalBranding {
	if uc == nil {
		return nil
	}
	uc.runtimeMu.RLock()
	config, client := uc.Config, uc.Client
	if config == nil || !config.LevelBadgeEnabled || client == nil {
		uc.runtimeMu.RUnlock()
		return nil
	}
	baseURL := config.NewAPIBaseURL
	uc.runtimeMu.RUnlock()
	profile, err := client.GetUserProfile(externalID)
	if err != nil {
		log.Debugf("pulse branding unavailable for %s: %v", externalID, err)
		return nil
	}
	return []*plugin.PersonalBranding{
		{Name: "pulse_level", Label: profile.Level.Name, Url: baseURL + "/console/pulse"},
		{Name: "pulse_contribution", Label: formatContribution(profile.LifetimeContributionMi), Url: baseURL + "/console/pulse"},
	}
}

func (uc *UserCenter) AfterLogin(externalID, _ string) {
	log.Debugf("pulse-bound forum user %s logged in", externalID)
}

func (uc *UserCenter) RegisterUnAuthRouter(r *gin.RouterGroup)   {}
func (uc *UserCenter) RegisterAuthUserRouter(r *gin.RouterGroup) {}

func (uc *UserCenter) RegisterAuthAdminRouter(r *gin.RouterGroup) {
	r.GET("/pulse/health", func(ctx *gin.Context) {
		config := uc.configSnapshot()
		ctx.JSON(http.StatusOK, gin.H{"pulse_base_url": config.PulseBaseURL})
	})
}

// formatContribution is display-only fixed-point formatting.
func formatContribution(milli int64) string { return strconv.FormatInt(milli/1000, 10) }
