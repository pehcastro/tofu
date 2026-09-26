package cat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestApprovedPoseDimensionsStayStable(t *testing.T) {
	tests := []struct {
		pose          Pose
		width, height int
	}{{Sitting, 26, 12}, {Lying, 36, 12}}
	for _, test := range tests {
		cat := New(WithPose(test.pose), WithPlain(true))
		for frame := range 12 {
			width, height := lipgloss.Size(cat.ViewFrame(frame))
			if width != test.width || height != test.height {
				t.Fatalf("pose %d frame %d = %dx%d, want %dx%d", test.pose, frame, width, height, test.width, test.height)
			}
		}
	}
}

func TestBothPosesAnimate(t *testing.T) {
	for _, pose := range []Pose{Sitting, Lying} {
		cat := New(WithPose(pose), WithPlain(true))
		first := cat.ViewFrame(0)
		changed := false
		for frame := 1; frame < 8; frame++ {
			changed = changed || cat.ViewFrame(frame) != first
		}
		if !changed {
			t.Fatalf("pose %d has no animated frame", pose)
		}
	}
}

func TestSittingCycleNeverUsesTheMalformedLeftTail(t *testing.T) {
	cat := New(WithPose(Sitting), WithPlain(true))
	if cat.ViewFrame(5) != cat.ViewFrame(0) {
		t.Fatal("sitting cycle does not return to the approved base frame")
	}
	if cat.ViewFrame(3) == cat.ViewFrame(0) {
		t.Fatal("clean right-tail frame is not animated")
	}
}

func TestSpritesMatchApprovedRootCapture(t *testing.T) {
	wantSittingTail := sprite{
		".X...S.......", ".XX.XS.......", ".XXXXS.......", "XGXGXXS......",
		".XXXXS.......", "...XS........", "..XWXS......S", "..XWXXS....S.",
		"..XXXXXS...S.", "..XXXXXS..S..", "..WXWXXXXS...", ".............",
	}
	wantLyingTail := sprite{
		".X...S............", ".XX.XS............", ".XXXXS..........S.", "XGXGXXS.XXX....S..",
		".XXXXSXXXXXX..S...", "..XWXXXXXXXXXS....", "WXWXXXXXWWWXXXS...", "..................",
		"..................", "..................", "..................", "..................",
	}
	if strings.Join(tailRight, "\n") != strings.Join(wantSittingTail, "\n") {
		t.Fatal("sitting tail frame differs from tofu-pose-options.png")
	}
	if strings.Join(lyingTail, "\n") != strings.Join(wantLyingTail, "\n") {
		t.Fatal("lying tail frame differs from tofu-pose-options.png")
	}
}

func TestPlainFallbackIsVisible(t *testing.T) {
	view := New(WithPlain(true)).View()
	if !strings.Contains(view, "##") || !strings.Contains(view, "oo") {
		t.Fatal("plain fallback has no visible body or eyes")
	}
}
