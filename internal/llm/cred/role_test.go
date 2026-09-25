package cred

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestARegisteredJevProviderMayNotSendAPrompt(t *testing.T) {
	for _, vendor := range []Vendor{OpenRouter, TypeSafe} {
		_, err := NewLLMKey(vendor, "a-secret-that-is-not-real")
		if err == nil {
			t.Fatalf("%s built a prompting credential, and it is registered as a %s", vendor, JevProvider)
		}
		if !strings.Contains(err.Error(), string(JevProvider)) {
			t.Fatalf("%s refused with %q, and the message must name the role", vendor, err)
		}
	}
}

func TestAJevProviderStillBuysATypedDecision(t *testing.T) {
	if _, err := NewJevKey(TypeSafe, " a-secret-that-is-not-real "); err != nil {
		t.Fatalf("NewJevKey: %v", err)
	}
}

func TestAnUnregisteredVendorBuysNothing(t *testing.T) {
	if _, err := NewJevKey(Vendor("anthropic"), "a-secret-that-is-not-real"); err == nil {
		t.Fatal("an unregistered vendor built a jev credential")
	}
}

func TestAJevKeyDoesNotCompileWhereAPromptIsSent(t *testing.T) {
	build := exec.Command("go", "build", "-o", os.DevNull, "./testdata/prompt_with_a_jev_key")
	out, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("the program compiled, so a jev key reaches a prompt:\n%s", out)
	}
	if !strings.Contains(string(out), "cannot use") {
		t.Fatalf("the build failed for another reason:\n%s", out)
	}
	t.Logf("the compiler refused it:\n%s", strings.TrimSpace(string(out)))
}
