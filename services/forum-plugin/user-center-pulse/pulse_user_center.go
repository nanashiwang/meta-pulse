package pulse_user_center

import (
	"embed"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/apache/answer-plugins/util"
	"github.com/apache/answer/plugin"
	"github.com/gin-gonic/gin"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/i18n"
	"github.com/segmentfault/pacman/log"
)

//go:embed info.yaml
var Info embed.FS

// UserCenter delegates all forum identity to new-api.
//
// Two deliberate constraints, both from docs/COMMUNITY.md:
//
//   - EnabledOriginalUserSystem is false: the forum has no independent
//     registration path, so a browser can never assert a user_id the way
//     AGENTS.md invariant 13 forbids.
//   - RankAgentEnabled is false: Answer's native rank gates moderation
//     privileges (voting, editing, closing). Handing that to Pulse would let
//     paid spend buy community governance power. The two reputations stay
//     separate and are only displayed side by side.
type UserCenter struct {
	Config *Config
	Client *PulseClient
	Nonces LoginTicketNonceStore
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
	return plugin.UserCenterDesc{
		Name:        "Meta Pulse",
		DisplayName: plugin.MakeTranslator(i18n.InfoName),
		Icon:        "",
		Url:         uc.Config.NewAPIBaseURL,

		LoginRedirectURL:  uc.Config.NewAPIBaseURL + "/api/forum/sso/start",
		SignUpRedirectURL: uc.Config.NewAPIBaseURL + "/register?next=%2Fapi%2Fforum%2Fsso%2Fstart",

		RankAgentEnabled:          false,
		UserStatusAgentEnabled:    true,
		UserRoleAgentEnabled:      false,
		MustAuthEmailEnabled:      false,
		EnabledOriginalUserSystem: false,
	}
}

func (uc *UserCenter) ControlCenterItems() []plugin.ControlCenter {
	return []plugin.ControlCenter{
		{
			Name:  "Meta Pulse",
			Label: "Meta Pulse",
			Url:   uc.Config.NewAPIBaseURL + "/console/pulse",
		},
		{
			Name:  "Console",
			Label: "API Console",
			Url:   uc.Config.NewAPIBaseURL + "/console",
		},
	}
}

// LoginCallback resolves a new-api login ticket into a forum user.
func (uc *UserCenter) LoginCallback(ctx *plugin.GinContext) (*plugin.UserCenterBasicUserInfo, error) {
	return uc.resolveUser(ctx)
}

// SignUpCallback is identical to login: accounts are always created upstream in
// new-api, never in the forum.
func (uc *UserCenter) SignUpCallback(ctx *plugin.GinContext) (*plugin.UserCenterBasicUserInfo, error) {
	return uc.resolveUser(ctx)
}

// resolveUser verifies the signed ticket new-api issued for this browser.
//
// This callback is reached by a browser redirect, so every field is attacker-
// controlled until the signature checks out. Nothing here may be trusted before
// Verify returns nil.
func (uc *UserCenter) resolveUser(ctx *gin.Context) (*plugin.UserCenterBasicUserInfo, error) {
	timestamp, err := strconv.ParseInt(ctx.Query("timestamp"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in login ticket")
	}

	ticket := &LoginTicket{
		UserID:      ctx.Query("user_id"),
		Username:    ctx.Query("username"),
		DisplayName: ctx.Query("display_name"),
		Email:       ctx.Query("email"),
		Avatar:      ctx.Query("avatar"),
		Timestamp:   timestamp,
		Nonce:       ctx.Query("nonce"),
		Signature:   ctx.Query("signature"),
	}

	secrets := []string{uc.Config.SSOHMACSecret}
	if previous := uc.Config.SSOHMACSecretPrevious; previous != "" && previous != uc.Config.SSOHMACSecret {
		secrets = append(secrets, previous)
	}
	if err := ticket.VerifyWithSecrets(ctx.Request.Context(), secrets, uc.Nonces, time.Now()); err != nil {
		log.Warnf("rejected user center login callback: %v", err)
		return nil, fmt.Errorf("login verification failed")
	}

	userInfo := &plugin.UserCenterBasicUserInfo{
		ExternalID:  ticket.UserID,
		Username:    ticket.Username,
		DisplayName: ticket.DisplayName,
		Email:       ticket.Email,
		Avatar:      ticket.Avatar,
		Status:      plugin.UserStatusAvailable,
	}
	if userInfo.DisplayName == "" {
		userInfo.DisplayName = userInfo.Username
	}
	return userInfo, nil
}

func (uc *UserCenter) UserInfo(externalID string) (*plugin.UserCenterBasicUserInfo, error) {
	return &plugin.UserCenterBasicUserInfo{
		ExternalID: externalID,
		Status:     uc.UserStatus(externalID),
	}, nil
}

// UserStatus deliberately does not derive account suspension from Pulse.
// new-api owns identity state; Pulse only supplies non-authoritative branding.
func (uc *UserCenter) UserStatus(string) plugin.UserStatus {
	return plugin.UserStatusAvailable
}

func (uc *UserCenter) UserList(externalIDs []string) ([]*plugin.UserCenterBasicUserInfo, error) {
	users := make([]*plugin.UserCenterBasicUserInfo, 0, len(externalIDs))
	for _, externalID := range externalIDs {
		users = append(users, &plugin.UserCenterBasicUserInfo{
			ExternalID: externalID,
			Status:     plugin.UserStatusAvailable,
		})
	}
	return users, nil
}

func (uc *UserCenter) UserSettings(externalID string) (*plugin.SettingInfo, error) {
	return &plugin.SettingInfo{
		ProfileSettingRedirectURL: uc.Config.NewAPIBaseURL + "/console/personal",
		AccountSettingRedirectURL: uc.Config.NewAPIBaseURL + "/console/personal",
	}, nil
}

// PersonalBranding renders the Pulse level as a profile badge.
//
// This is the whole point of the integration: paid usage earns a level in
// Pulse, and the level becomes visible social standing in the forum.
func (uc *UserCenter) PersonalBranding(externalID string) []*plugin.PersonalBranding {
	if !uc.Config.LevelBadgeEnabled {
		return nil
	}

	profile, err := uc.Client.GetUserProfile(externalID)
	if err != nil {
		log.Debugf("pulse branding unavailable for %s: %v", externalID, err)
		return nil
	}

	return []*plugin.PersonalBranding{
		{
			Name:  "pulse_level",
			Label: profile.Level.Name,
			Url:   uc.Config.NewAPIBaseURL + "/console/pulse",
		},
		{
			Name:  "pulse_contribution",
			Label: formatContribution(profile.LifetimeContributionMi),
			Url:   uc.Config.NewAPIBaseURL + "/console/pulse",
		},
	}
}

func (uc *UserCenter) AfterLogin(externalID, accessToken string) {
	log.Debugf("pulse user center: user %s logged in", externalID)
}

func (uc *UserCenter) RegisterUnAuthRouter(r *gin.RouterGroup) {}

func (uc *UserCenter) RegisterAuthUserRouter(r *gin.RouterGroup) {}

func (uc *UserCenter) RegisterAuthAdminRouter(r *gin.RouterGroup) {
	r.GET("/pulse/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"pulse_base_url": uc.Config.PulseBaseURL})
	})
}

// formatContribution renders fixed-point contribution_milli for display only.
// Never use this value for accounting; Pulse's ledger is the source of truth.
func formatContribution(milli int64) string {
	return strconv.FormatInt(milli/1000, 10)
}
