package redis

import (
	"strings"
	"testing"
)

func TestURLTransportAndLegacy(t *testing.T) {
	c, err := OpenConfigured("rediss://alice:secret@example.invalid:6380/2", "ignored", "ignored", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	o := c.Raw().Options()
	if o.Username != "alice" || o.Password != "secret" || o.DB != 2 || o.TLSConfig == nil || o.TLSConfig.InsecureSkipVerify {
		t.Fatal("URL authentication or verified TLS lost")
	}
	legacy, err := OpenConfigured("", "localhost:6379", "old-password", 3)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if legacy.Raw().Options().DB != 3 || legacy.Raw().Options().Password != "old-password" {
		t.Fatal("legacy configuration changed")
	}
	for _, url := range []string{"https://user:secret@invalid/", "rediss://user:secret@invalid?skip_verify=true", "redis://user:secret@invalid/NaN", "rediss://user:secret@invalid?unknown=1"} {
		_, err = OpenConfigured(url, "fallback:6379", "", 0)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid URL accepted or secret leaked")
		}
	}
}
