package corpus

import (
	"errors"
	"testing"
)

func TestResolvePinSurvivesLineInsertedAbove(t *testing.T) {
	before := []string{"package p", "", "func Foo() {", "return", "}"}
	pin := Pin{Answer: Answer{File: "x.go", Line: 3}, Fingerprint: fingerprintOf(before[2])}
	after := append([]string{"import \"fmt\""}, before...)
	resolution, err := ResolvePin(after, pin)
	if err != nil {
		t.Fatalf("ResolvePin: %v", err)
	}
	if resolution.Line != 4 {
		t.Fatalf("expected the shifted content at line 4, got %d", resolution.Line)
	}
	if !resolution.Moved {
		t.Fatal("expected Moved once the hint no longer names the content's own line")
	}
}

func TestResolvePinBreaksWhenContentChanges(t *testing.T) {
	lines := []string{"package p", "func Foo() {", "return 1", "}"}
	pin := Pin{Answer: Answer{File: "x.go", Line: 3}, Fingerprint: fingerprintOf(lines[2])}
	lines[2] = "return 2"
	_, err := ResolvePin(lines, pin)
	if !errors.Is(err, ErrPinContentChanged) {
		t.Fatalf("expected ErrPinContentChanged, got %v", err)
	}
}

func TestResolvePinRefusesAmbiguousContent(t *testing.T) {
	target := "return nil"
	fingerprint := fingerprintOf(target)
	lines := []string{"changed", "x := 1", target, "y := 2", target}
	pin := Pin{Answer: Answer{File: "x.go", Line: 1}, Fingerprint: fingerprint}
	_, err := ResolvePin(lines, pin)
	if !errors.Is(err, ErrPinAmbiguous) {
		t.Fatalf("expected ErrPinAmbiguous, got %v", err)
	}
}

func TestResolvePinUnchangedHintSkipsTheSearch(t *testing.T) {
	target := "return nil"
	fingerprint := fingerprintOf(target)
	lines := []string{"changed", "x := 1", target, "y := 2", target}
	pin := Pin{Answer: Answer{File: "x.go", Line: 3}, Fingerprint: fingerprint}
	resolution, err := ResolvePin(lines, pin)
	if err != nil {
		t.Fatalf("ResolvePin: %v", err)
	}
	if resolution.Line != 3 || resolution.Moved {
		t.Fatalf("a hint that still matches its own line must not be treated as ambiguous or moved, got %+v", resolution)
	}
}
