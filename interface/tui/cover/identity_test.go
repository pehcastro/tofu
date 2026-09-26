package cover

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestIdentityUsesApprovedPoseCompositions(t *testing.T) {
	identity := NewIdentity(false)
	if poses[0].x != -17 || poses[0].y != 2 || poses[1].x != -11 || poses[1].y != 6 {
		t.Fatal("approved pose placement changed")
	}
	for range 2 {
		width, height := lipgloss.Size(identity.View(stageWidth))
		if width > stageWidth || height != 20 {
			t.Fatalf("identity dimensions = %dx%d, want <=68x20", width, height)
		}
		identity.TogglePose()
	}
}

func TestIdentityKeepsChosenPoseWhileAnimating(t *testing.T) {
	identity := NewIdentity(true)
	identity.TogglePose()
	for range 100 {
		identity, _ = identity.Update(pulseMsg{})
	}
	if identity.PoseName() != "lying" {
		t.Fatal("chosen pose changed during the cover animation")
	}
	if identity.cat.ViewFrame(0) == identity.cat.ViewFrame(2) {
		t.Fatal("lying pose lost its own animated tail frames")
	}
}

func TestWordmarkAndOrbitAreStable(t *testing.T) {
	if width, height := lipgloss.Size(wordmark(0)); width != 30 || height != 6 {
		t.Fatalf("wordmark = %dx%d, want 30x6", width, height)
	}
	if wordmark(0) == wordmark(1) {
		t.Fatal("orbit pixel did not move")
	}
}

func TestASCIIWordmarkKeepsDimensionsAndUsesASCII(t *testing.T) {
	view := asciiWordmark(8)
	if width, height := lipgloss.Size(view); width != 30 || height != 6 {
		t.Fatalf("ASCII wordmark = %dx%d, want 30x6", width, height)
	}
	for _, char := range view {
		if char > 127 {
			t.Fatalf("ASCII wordmark retained %q", char)
		}
	}
}

func TestPlacementPreservesVisibleRowWidths(t *testing.T) {
	placed := strings.Split(place(20, 4, "  XX  \n  YY  \n      ", 0, 0), "\n")
	if lipgloss.Width(placed[0]) != lipgloss.Width(placed[1]) {
		t.Fatalf("visible rows were independently shifted: widths %d and %d", lipgloss.Width(placed[0]), lipgloss.Width(placed[1]))
	}
	if !strings.HasSuffix(placed[0], "  ") || !strings.HasSuffix(placed[1], "  ") {
		t.Fatal("placement stripped the visible rows' alignment padding")
	}
}
