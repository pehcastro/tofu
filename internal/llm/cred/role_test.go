package cred

import (
	"strings"
	"testing"
)

func TestARegisteredClassifierVendorMayNotSendAPrompt(t *testing.T) {
	for _, vendor := range []Vendor{OpenRouter, TypeSafe} {
		_, err := NewLLMKey(vendor, "a-secret-that-is-not-real")
		if err == nil {
			t.Fatalf("%s built a prompting credential, and it is registered as %s", vendor, RoleClassifier)
		}
		if !strings.Contains(err.Error(), string(RoleLLM)) || !strings.Contains(err.Error(), string(RoleClassifier)) {
			t.Fatalf("%s refused with %q, and the message must name both the kind asked for and the kind held", vendor, err)
		}
	}
}
