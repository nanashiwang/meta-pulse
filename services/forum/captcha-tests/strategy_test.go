package action

import (
	"context"
	"testing"

	"github.com/apache/answer/internal/entity"
	"github.com/apache/answer/plugin"
)

type metarCaptcha struct{ calls int }

func (*metarCaptcha) Info() plugin.Info             { return plugin.Info{SlugName: "metar_test_captcha"} }
func (*metarCaptcha) GetConfig() string             { return "{}" }
func (*metarCaptcha) Create() (string, string)      { return "", "" }
func (c *metarCaptcha) Verify(_, token string) bool { c.calls++; return token == "verified" }

type metarRepo struct{}

func (metarRepo) SetCaptcha(context.Context, string, string) error                 { return nil }
func (metarRepo) GetCaptcha(context.Context, string) (string, error)               { return "", nil }
func (metarRepo) DelCaptcha(context.Context, string) error                         { return nil }
func (metarRepo) SetActionType(context.Context, string, string, string, int) error { return nil }
func (metarRepo) GetActionType(context.Context, string, string) (*entity.ActionRecordInfo, error) {
	return nil, nil
}
func (metarRepo) DelActionType(context.Context, string, string) error { return nil }
func TestMetarEmailAlwaysValidatesCaptcha(t *testing.T) {
	captcha := &metarCaptcha{}
	plugin.Register(captcha)
	plugin.StatusManager.Enable("metar_test_captcha", true)
	defer plugin.StatusManager.Enable("metar_test_captcha", false)
	service := NewCaptchaService(metarRepo{})
	ctx := context.Background()
	for _, token := range []string{"", "forged", "verified"} {
		if got := service.ActionRecordVerifyCaptcha(ctx, entity.CaptchaActionEmail, "127.0.0.1", "", token); got != (token == "verified") {
			t.Fatalf("unexpected registration verification for %q: %v", token, got)
		}
	}
	if captcha.calls != 3 {
		t.Fatalf("registration bypassed plugin: calls=%d", captcha.calls)
	}
	if !service.ValidationStrategy(ctx, "127.0.0.1", entity.CaptchaActionPassword) {
		t.Fatal("ordinary first login should retain native policy")
	}
}
