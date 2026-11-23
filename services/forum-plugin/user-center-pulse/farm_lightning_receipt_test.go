package pulse_user_center

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestFarmReceiptLightningAndLegacyDomains(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(pub)
	pin := hex.EncodeToString(digest[:])
	order := strings.Repeat("1", 32)
	makeReceipt := func(version, price string) http.Header {
		msg := strings.Join([]string{"meta-pulse:token-farm:paid:" + version, "7", "42", order, price}, "\n")
		return http.Header{"X-Farm-Receipt": {version + "." + hex.EncodeToString(pub) + "." + hex.EncodeToString(ed25519.Sign(priv, []byte(msg)))}}
	}
	for _, c := range []struct {
		version, price string
		ok             bool
	}{{"v2", "10000:1", true}, {"v3", "500000:1:lightning_milli", true}, {"v3", "500:1", false}} {
		h := makeReceipt(c.version, c.price)
		if verifyFarmReceipt(h, "7", "42", order, pin) != c.ok {
			t.Fatal(c.version, c.price)
		}
		if verifyFarmReceipt(h, "7", "43", order, pin) {
			t.Fatal("accepted mismatched identity")
		}
	}
}
