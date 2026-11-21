package pulse_user_center

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"regexp"
)

var farmOrderPattern = regexp.MustCompile("^[0-9a-f]{32}$")

// This proof is generated only after the farm has durably debited the order.
// It may be replayed: Pulse permanently deduplicates the order across users.
func paidFarmOrder(headers http.Header, forum, subject, order string) bool {
	key, err := hex.DecodeString(os.Getenv("TOKEN_FARM_EXCHANGE_KEY"))
	if err != nil || len(key) != 32 || len(headers.Values("X-Farm-Receipt")) != 1 {
		return false
	}
	sig, err := hex.DecodeString(headers.Get("X-Farm-Receipt"))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("farm-ticket:v1\n" + forum + "\n" + subject + "\n" + order + "\n10000:1"))
	return hmac.Equal(sig, mac.Sum(nil))
}
