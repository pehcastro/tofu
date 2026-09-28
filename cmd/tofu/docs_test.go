package main

import (
	"bytes"
	"strings"
	"testing"

	"tofu/internal/settings"
)

func TestDocsSettingsNamesEveryDeclaredKey(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"docs", "settings"}, strings.NewReader(""), &out, &errOut); code != exitOK {
		t.Fatalf("tofu docs settings exited %d: %s", code, errOut.String())
	}
	for _, spec := range settings.Default() {
		if !strings.Contains(out.String(), "\n  "+spec.Key+" = ") {
			t.Errorf("tofu docs settings has no table line for %s", spec.Key)
		}
	}
}
