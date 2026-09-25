package sys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	CredentialFileName  = ".env"
	liveCredentialOptIn = "TOFU_TEST_LIVE_CREDENTIAL"
)

var builtGuarded string

func AllowLiveCredential(tb testing.TB) {
	tb.Setenv(liveCredentialOptIn, "1")
}

func CredentialsHiddenFromTests() bool {
	return (testing.Testing() || builtGuarded != "") && os.Getenv(liveCredentialOptIn) != "1"
}

func ReadCredential(path string) ([]byte, error) {
	if err := refuseOwnerCredential(path); err != nil {
		return nil, err
	}
	return readCredentialFile(path)
}

func readCredentialFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func WriteCredential(path string, data []byte, perm os.FileMode) error {
	if err := refuseOwnerCredential(path); err != nil {
		return err
	}
	return WriteFile(path, data, perm)
}

func refuseOwnerCredential(path string) error {
	if !CredentialsHiddenFromTests() {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if !IsOwnerCredential(abs) {
		return nil
	}
	return fmt.Errorf("sys: %s is hidden from tests: it is one of the owner's credential files, and a test that means to use it calls sys.AllowLiveCredential first", abs)
}

func PlantOwnerCredential(tb testing.TB, name string) bool {
	value := ownerCredentialValue(name)
	if value == "" {
		return false
	}
	tb.Setenv(name, value)
	return true
}

func EqualsOwnerCredential(name, value string) bool {
	return value != "" && value == ownerCredentialValue(name)
}

func ownerCredentialValue(name string) string {
	for _, dir := range []string{SourceRoot(), OwnerHomeStateDir()} {
		if dir == "" {
			continue
		}
		raw, err := readCredentialFile(filepath.Join(dir, CredentialFileName))
		if err != nil {
			continue
		}
		if value := CredentialAssignment(string(raw), name); value != "" {
			return value
		}
	}
	return ""
}

func CredentialAssignment(body, name string) string {
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		value, found := strings.CutPrefix(line, name+"=")
		if !found {
			continue
		}
		if value = strings.Trim(strings.TrimSpace(value), `"'`); value != "" {
			return value
		}
	}
	return ""
}
