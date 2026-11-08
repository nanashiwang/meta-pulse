package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDryRunValidatesExampleWithoutDatabase(t *testing.T) {
	t.Setenv("INTEL_DB_DSN", "")
	var out bytes.Buffer
	if err := run([]string{"import", "--dry-run", "../../testdata/example-import.json"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 models") || !strings.Contains(out.String(), "6 results") {
		t.Fatalf("unexpected summary: %s", out.String())
	}
}

func TestReadDocumentRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"typo.json":  `{"source":{"slug":"x"},"modles":[]}`,
		"two.json":   `{"source":{"slug":"x"}} {}`,
		"price.json": `{"source":{"slug":"x"},"prices":[{"offering":"o","input_usd_per_mtok":2.5}]}`,
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readDocument(p); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	if err := run(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("empty args accepted")
	}
}
