package web

import (
	"testing"

	"tofu/internal/sys"
)

func TestCredentialIsHiddenFromATestStandingInTheCheckout(t *testing.T) {
	variable := "OPENROUTER" + "_KEY"
	t.Setenv(variable, "")
	t.Chdir(sys.SourceRoot())
	value, found := credential(variable)
	if found || value != "" {
		t.Fatalf("a test read a credential of length %d out of the source tree .env", len(value))
	}
}

func TestCredentialStillReadsAnEnvFileThatIsNobodys(t *testing.T) {
	variable := "SEARCH" + "_KEY"
	t.Setenv(variable, "")
	dir := t.TempDir()
	if err := sys.WriteCredential(sys.Join(dir, sys.CredentialFileName), []byte(variable+"=a-search-key\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Chdir(dir)
	value, found := credential(variable)
	if !found || value != "a-search-key" {
		t.Fatalf("credential from a temporary .env = %q, %v", value, found)
	}
}
