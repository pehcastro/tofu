package markdown

import "testing"

func TestBoundaryPromotesAHeadingOnceItsBlankLineArrives(t *testing.T) {
	content := "# Title\n\nBody streaming so"
	boundary := Boundary(content)
	if boundary != len("# Title\n\n") {
		t.Fatalf("boundary %d, want the heading closed at %d", boundary, len("# Title\n\n"))
	}
}

func TestBoundaryHoldsAtAnOpenFenceUntilItCloses(t *testing.T) {
	steps := []string{
		"Intro\n\n```go\ncode",
		"Intro\n\n```go\ncode line two",
		"Intro\n\n```go\ncode line two\n```\n",
		"Intro\n\n```go\ncode line two\n```\n\nAfter",
	}
	held := len("Intro\n\n")
	boundaries := make([]int, len(steps))
	for index, content := range steps {
		boundaries[index] = Boundary(content)
	}
	if boundaries[0] != held || boundaries[1] != held {
		t.Fatalf("boundaries while the fence is open: %v, want %d held", boundaries[:2], held)
	}
	closed := len("Intro\n\n```go\ncode line two\n```\n")
	if boundaries[2] != closed {
		t.Fatalf("boundary once the fence closes = %d, want %d", boundaries[2], closed)
	}
	if boundaries[3] <= boundaries[2] {
		t.Fatalf("boundary after more text arrives = %d, want it to grow past %d", boundaries[3], boundaries[2])
	}
}

func TestBoundaryNeverRegresses(t *testing.T) {
	safe := "Intro\n\n"
	unsafe := safe + "```go\nstill open"
	if b := Boundary(unsafe); b > len(safe) {
		t.Fatalf("boundary inside an unclosed fence = %d, want no more than %d", b, len(safe))
	}
}

func TestBoundaryStaysAtZeroWithNoParagraphBreakYet(t *testing.T) {
	if b := Boundary("still one growing paragraph, no break"); b != 0 {
		t.Fatalf("boundary %d, want 0 with nothing closed yet", b)
	}
}
