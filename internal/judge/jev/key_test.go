package jev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/transport"
)

func keyName() string { return "OPENROUTER" + "_KEY" }

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolving %s: %v", path, err)
	}
	return abs
}

func TestKeyPrefersTheEnvironment(t *testing.T) {
	t.Setenv(keyName(), "from-the-environment")
	got, err := Key("")
	if err != nil {
		t.Fatalf("reading the key: %v", err)
	}
	if got != "from-the-environment" {
		t.Fatalf("expected the environment value, got a value of length %d", len(got))
	}
}

func TestKeyFallsBackToTheEnvFile(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "# a comment\n\nOTHER=1\nexport " + keyName() + "=\"from-the-file\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Key(path)
	if err != nil {
		t.Fatalf("reading the key: %v", err)
	}
	if got != "from-the-file" {
		t.Fatalf("expected the file value, got a value of length %d", len(got))
	}
}

func TestKeyFailsWhenThereIsNone(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	_, err := Key(path)
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
	if !transport.KindOf(err).Fatal() {
		t.Fatal("a missing credential must be fatal")
	}
}

func TestLocateFromTheEnvironment(t *testing.T) {
	t.Setenv(keyName(), "from-the-environment")
	got, err := Locate("")
	if err != nil {
		t.Fatalf("locating the key: %v", err)
	}
	if got.Source != SourceEnvironment {
		t.Fatalf("expected SourceEnvironment, got %v", got.Source)
	}
	if got.Length != len("from-the-environment") {
		t.Fatalf("expected the length to match, got %d", got.Length)
	}
}

func TestLocateFromTheEnvFile(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := keyName() + "=from-the-file\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Locate(path)
	if err != nil {
		t.Fatalf("locating the key: %v", err)
	}
	if got.Source != SourceDotEnv {
		t.Fatalf("expected SourceDotEnv, got %v", got.Source)
	}
	want := mustAbs(t, path)
	if got.Path != want {
		t.Fatalf("expected the resolved path %s, got %s", want, got.Path)
	}
	if got.Length != len("from-the-file") {
		t.Fatalf("expected the length to match, got %d", got.Length)
	}
}

func TestLocateWhenThereIsNone(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("OTHER=1\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	got, err := Locate(path)
	if err == nil {
		t.Fatal("expected the missing key to be refused")
	}
	if got.Source != SourceMissing {
		t.Fatalf("expected SourceMissing, got %v", got.Source)
	}
	want := mustAbs(t, path)
	if got.Path != want {
		t.Fatalf("expected the resolved path %s, got %s", want, got.Path)
	}
}

func TestKeyNeverPutsTheValueInAnError(t *testing.T) {
	t.Setenv(keyName(), "")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(keyName()+"=\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	_, err := Key(path)
	if err == nil {
		t.Fatal("expected an empty value to be refused")
	}
	if strings.Contains(err.Error(), "=") {
		t.Fatalf("the error looks like it carries an assignment: %v", err)
	}
}
