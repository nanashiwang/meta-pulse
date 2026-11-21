package pulse_user_center

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
)

var farmOrderPattern = regexp.MustCompile("^[0-9a-f]{32}$")

// Non-secret pin supplied by the operator; no signing authority resides here.
const farmSignerPin = "c022cc9a6a0ace05d5ec5a6bca9c1cc2717117c2a612b4ff038fd79d64bbdfa6"

func verifyFarmReceipt(headers http.Header, forum, subject, order, pin string) bool {
	values := headers.Values("X-Farm-Receipt")
	if len(values) != 1 || len(values[0]) != 3+64+1+128 || !farmOrderPattern.MatchString(order) {
		return false
	}
	parts := strings.Split(values[0], ".")
	if len(parts) != 3 || parts[0] != "v2" {
		return false
	}
	pub, e := hex.DecodeString(parts[1])
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	digest := sha256.Sum256(pub)
	if hex.EncodeToString(digest[:]) != pin {
		return false
	}
	sig, e := hex.DecodeString(parts[2])
	if e != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	message := strings.Join([]string{"meta-pulse:token-farm:paid:v2", forum, subject, order, "10000:1"}, "\n")
	return ed25519.Verify(ed25519.PublicKey(pub), []byte(message), sig)
}
