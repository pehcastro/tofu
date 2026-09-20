package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogNamesEveryKindAndItsContract(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := catalogVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("tofu catalog exited %d: %s\n%s", code, errOut.String(), out.String())
	}
	t.Logf("tofu catalog\n%s", out.String())
	text := out.String()
	for _, want := range []string{
		"models", "subscriptions", "roles", "questions", "policy", "rules",
		"required subscription, use", "required provider, wire, windows", "required model",
		"winning field by field",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tofu catalog never says %q:\n%s", want, text)
		}
	}
}

func TestCatalogCountsTheRolesTheProjectBound(t *testing.T) {
	project := t.TempDir()
	writeProjectRole(t, project, "turn", "anthropic/claude-sonnet-5")
	t.Chdir(project)

	var out, errOut bytes.Buffer
	if code := catalogVerb(nil, &out, &errOut); code != exitOK {
		t.Fatalf("tofu catalog exited %d: %s\n%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "roles            1 loaded") {
		t.Fatalf("tofu catalog does not count the bound role:\n%s", out.String())
	}
}

func TestCatalogNamesTheFileAndTheFieldItRefused(t *testing.T) {
	project := t.TempDir()
	broken := filepath.Join(project, ".boji", "models", "openai")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatalf("building a project catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(broken, "gpt-5.6-terra.yaml"), []byte("use: excluded\n"), 0o644); err != nil {
		t.Fatalf("writing the broken entry: %v", err)
	}
	t.Chdir(project)

	var out, errOut bytes.Buffer
	if code := catalogVerb(nil, &out, &errOut); code != exitVerdict {
		t.Fatalf("a broken entry must fail the verb, got %d\n%s", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "gpt-5.6-terra.yaml") || !strings.Contains(text, "reason") {
		t.Fatalf("tofu catalog does not name the file and the field:\n%s", text)
	}
}

func TestProjectCatalogOverridesTheShippedOneFieldByField(t *testing.T) {
	project := t.TempDir()
	dir := filepath.Join(project, ".boji", "models", "anthropic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("building a project catalog: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude-fable-5.yaml"), []byte("use: allowed\n"), 0o644); err != nil {
		t.Fatalf("writing the project entry: %v", err)
	}
	t.Chdir(project)

	model, err := selectModel("anthropic", "anthropic/claude-fable-5")
	if err != nil {
		t.Fatalf("the project layer did not relax the exclusion: %v", err)
	}
	if model.Subscription != "claude" || model.WindowText() != "5h and 7d and 7d:fable" {
		t.Fatalf("the project file replaced facts it never mentioned: %+v", model)
	}
}
