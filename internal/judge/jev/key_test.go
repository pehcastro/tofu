package jev

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
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

func TestATestThatDoesNotOptInReachesNoCredentialInTheSourceTree(t *testing.T) {
	t.Setenv(keyName(), "")
	t.Chdir(sys.SourceRoot())
	key, err := Key(".env")
	if key != "" {
		t.Fatalf("a test read a credential of length %d out of the source tree", len(key))
	}
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
}

func TestATestThatDoesNotOptInReachesNoCredentialInTheHomeStateDirectory(t *testing.T) {
	t.Setenv(keyName(), "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("skipped, not counted as a pass: this machine reports no home directory (%v)", err)
	}
	if strings.HasPrefix(home, os.TempDir()) {
		t.Skipf("skipped, not counted as a pass: the home directory %s is a temporary one, so it is nobody's credential store", home)
	}
	path := filepath.Join(sys.StateDir(home), ".env")
	key, keyErr := Key(path)
	if key != "" {
		t.Fatalf("a test read a credential of length %d out of the home state directory", len(key))
	}
	if keyErr == nil || !strings.Contains(keyErr.Error(), "hidden from tests") {
		t.Fatalf("expected %s to be hidden from tests, got %v", path, keyErr)
	}
}

func TestATestThatDoesNotOptInReachesNoCredentialItPlantedFromTheOwnersOwn(t *testing.T) {
	if !sys.PlantOwnerCredential(t, keyName()) {
		t.Skipf("skipped, not counted as a pass: this machine carries no %s in the source tree .env", keyName())
	}
	key, err := Key("")
	if key != "" {
		t.Fatalf("a test read the owner credential of length %d out of the environment", len(key))
	}
	if transport.KindOf(err) != transport.KindMissingCredential {
		t.Fatalf("expected kind missing_credential, got %v", err)
	}
}

func TestATestThatOptsInByNameReachesTheCredentialAgain(t *testing.T) {
	AllowLiveCredential(t)
	t.Setenv(keyName(), "")
	root := sys.SourceRoot()
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		t.Skipf("skipped, not counted as a pass: this machine has no .env at %s (%v)", root, err)
	}
	t.Chdir(root)
	key, err := Key(".env")
	if err != nil {
		t.Fatalf("an opted-in test was refused: %v", err)
	}
	if key == "" {
		t.Fatal("an opted-in test read an empty credential")
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
