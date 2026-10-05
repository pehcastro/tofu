package keyfield

import (
	"strings"
	"testing"

	"tofu/internal/sys"
)

func revealed(shape, secret, prefix string) int {
	count := 0
	for _, part := range strings.Split(strings.TrimPrefix(strings.Fields(shape)[0], prefix), ellipsis) {
		if part != "" && strings.Contains(secret, part) {
			count += len(part)
		}
	}
	return count
}

func TestTheShapeNeverShowsMoreThanHalfOfTheSecret(t *testing.T) {
	for _, variable := range []string{sys.OpenRouterKeyName, sys.BraveSearchKeyName} {
		field, prefix := New(variable), expectedPrefix(variable)
		for _, r := range "sk-or-v1-q8Zr2mW4kT7vX1nB5cY9pL3dF6hJ0aS" {
			field.Type(string(r))
			secret := strings.TrimPrefix(field.Value(), prefix)
			if shown := revealed(field.Shape(), secret, prefix); shown > 0 && 2*shown > len(secret) {
				t.Fatalf("%s at %d characters shows %d of them: %q", variable, len(secret), shown, field.Shape())
			}
		}
	}
}

func TestAPasteKeepsNoLineBreakAndTheShapeSaysTheLength(t *testing.T) {
	field := New(sys.OpenRouterKeyName)
	field.Paste("sk-or-v1-made-up-0000000000003f9a\r\n")
	if field.Value() != "sk-or-v1-made-up-0000000000003f9a" {
		t.Fatalf("the pasted value is %q", field.Value())
	}
	if want := "sk-or-v1-…3f9a  33 chars  prefix ok"; field.Shape() != want {
		t.Fatalf("the shape is %q, want %q", field.Shape(), want)
	}
}

func TestAWrongPrefixAsksOnceAndTypingAsksAgain(t *testing.T) {
	field := New(sys.OpenRouterKeyName)
	field.Paste("sk-proj-made-up-00000000000000")
	if field.Enter() || field.Warning() == "" {
		t.Fatal("the first enter on a wrong prefix saved without a warning")
	}
	field.Type("1")
	if field.Enter() {
		t.Fatal("a key typed after the warning saved on the next enter")
	}
	if !field.Enter() {
		t.Fatal("the second enter on a wrong prefix did not save")
	}
}

func TestAnUncheckedPrefixSaysSoAndNeverAsks(t *testing.T) {
	field := New(sys.MetaMuseKeyName)
	field.Paste("made-up-meta-key-00000000000000")
	if !strings.HasSuffix(field.Shape(), "prefix not checked") || !field.Enter() {
		t.Fatalf("an unchecked prefix says %q", field.Shape())
	}
}
