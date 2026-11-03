package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"tofu/apps/cli/output"
	libraryverb "tofu/apps/cli/verbs/library"
	"tofu/internal/store/settings"
)

func TestDocsSettingsNamesEveryDeclaredKey(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if code := run([]string{"docs", "settings"}, strings.NewReader(""), &out, &errOut); code != output.ExitOK {
		t.Fatalf("tofu docs settings exited %d: %s", code, errOut.String())
	}
	rows := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) > 1 {
			rows[fields[0]] = true
		}
	}
	for _, spec := range settings.Default() {
		if !rows[spec.Key] {
			t.Errorf("tofu docs settings has no table row for %s", spec.Key)
		}
	}
}

func TestDocsReportsARoutedVerbNoPageNames(t *testing.T) {
	listed := usageVerbs()
	for _, verb := range routedVerbs(t) {
		if strings.Contains(usage, "\n  "+verb+" ") && !slices.Contains(listed, verb) {
			t.Errorf("tofu routes %q and the docs check never asks for its page", verb)
		}
	}
	_, missing := libraryverb.DocsCoverage(append(listed, "frobnicate"))
	if len(missing) != 1 || !strings.Contains(missing[0].Error(), "the verb frobnicate") {
		t.Fatalf("a routed verb no page names produced %v, want one line naming frobnicate", missing)
	}
	t.Logf("refused: %v", missing[0])
}
