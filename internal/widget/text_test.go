package widget

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMaskNeverRevealsMoreThanFourRunesOrMostOfTheSecret(t *testing.T) {
	alphabet := []rune("abcdefghijklmnopqrstuvwxyz0123456789ABCD")
	for length := 0; length <= len(alphabet); length++ {
		secret := string(alphabet[:length])
		got := Mask(secret)
		revealed, masked := strings.CutPrefix(got, maskFill)
		if !masked {
			t.Fatalf("a secret of %d runes is not masked at all: %q", length, got)
		}
		switch shown := len([]rune(revealed)); {
		case shown > maskedRunes:
			t.Errorf("a secret of %d runes reveals %d runes: %q", length, shown, got)
		case shown*2 > length:
			t.Errorf("a secret of %d runes reveals %d of them, which is most of it: %q", length, shown, got)
		case revealed != "" && !strings.HasSuffix(secret, revealed):
			t.Errorf("a secret of %d runes reveals something that is not its tail: %q", length, got)
		}
	}
}

func TestASecretTooShortToSpareATailIsMaskedWhole(t *testing.T) {
	for _, secret := range []string{"", "a", "1234", "12345", "12345678"} {
		if got := Mask(secret); got != maskFill {
			t.Errorf("the secret %q shows %q, and a secret of %d runes has no tail to spare",
				secret, got, len([]rune(secret)))
		}
	}
	for _, step := range []struct{ secret, want string }{
		{"123456789", maskFill + "6789"},
		{"sk-or-v1-77c1f0b6e5a94d2f8badc0ffee1234567890abcd", maskFill + "abcd"},
		{"αβγδεζηθικλμ", maskFill + "ικλμ"},
	} {
		if got := Mask(step.secret); got != step.want {
			t.Errorf("Mask(%q) = %q, want %q", step.secret, got, step.want)
		}
	}
}

const (
	zeroWidthJoiner = 0x200d
	man             = 0x1f468
	woman           = 0x1f469
	girl            = 0x1f467
	twoCells        = "日"
)

func joinedEmoji() string {
	return string([]rune{man, zeroWidthJoiner, woman, zeroWidthJoiner, girl})
}

func TestFitCountsDisplayCellsAndMarksTheCut(t *testing.T) {
	family := joinedEmoji()
	for _, step := range []struct {
		text  string
		width int
		want  string
	}{
		{"boji", 0, ""},
		{"boji", -3, ""},
		{"boji", 1, ellipsis},
		{"b", 1, "b"},
		{"boji", 4, "boji"},
		{"boji", 9, "boji"},
		{"boji", 3, "bo" + ellipsis},
		{"ключ", 4, "ключ"},
		{"ключ", 3, "кл" + ellipsis},
		{"日本語です", 10, "日本語です"},
		{"日本語です", 9, "日本語で" + ellipsis},
		{"日本語です", 4, "日" + ellipsis},
		{family + " boji", 7, family + " boji"},
		{family + " boji", 6, family + " bo" + ellipsis},
		{family, 1, ellipsis},
	} {
		if got := Fit(step.text, step.width); got != step.want {
			t.Errorf("Fit(%q, %d) = %q, want %q", step.text, step.width, got, step.want)
		}
	}
}

func TestACutNeverLandsInsideACharacterAndLeavesTheSpareCellEmpty(t *testing.T) {
	const text = "日本語です"
	for width := 1; width <= Cells(text); width++ {
		got := Fit(text, width)
		if Cells(got) > width {
			t.Errorf("Fit(%q, %d) = %q, which is %d cells wide", text, width, got, Cells(got))
		}
		kept := strings.TrimSuffix(got, ellipsis)
		if !strings.HasPrefix(text, kept) {
			t.Errorf("Fit(%q, %d) = %q, which keeps %q and that is not a whole prefix", text, width, got, kept)
		}
	}
	if got := Fit(text, 5); got != "日本"+ellipsis {
		t.Errorf("at an odd budget with a two-cell rune at the boundary Fit gave %q", got)
	}
	if got := Fit(text, 4); got != twoCells+ellipsis {
		t.Errorf("a two-cell rune that does not fit was cut rather than dropped: %q", got)
	}
}

func TestPadAndLeadCountCellsAndNeverShortenAnything(t *testing.T) {
	family := joinedEmoji()
	for _, step := range []struct {
		text            string
		width           int
		padded, leading string
	}{
		{"", 0, "", ""},
		{"", 2, "  ", "  "},
		{"boji", 0, "boji", "boji"},
		{"boji", 2, "boji", "boji"},
		{"boji", 4, "boji", "boji"},
		{"boji", 6, "boji  ", "  boji"},
		{"ключ", 6, "ключ  ", "  ключ"},
		{"日本", 4, "日本", "日本"},
		{"日本", 6, "日本  ", "  日本"},
		{family, 4, family + "  ", "  " + family},
	} {
		if got := Pad(step.text, step.width); got != step.padded {
			t.Errorf("Pad(%q, %d) = %q, want %q", step.text, step.width, got, step.padded)
		}
		if got := Lead(step.text, step.width); got != step.leading {
			t.Errorf("Lead(%q, %d) = %q, want %q", step.text, step.width, got, step.leading)
		}
	}
}

func TestWrapKeepsEveryLineInsideTheColumn(t *testing.T) {
	family := joinedEmoji()
	for _, step := range []struct {
		text  string
		width int
		want  []string
	}{
		{"press 1 to run", 0, nil},
		{"press 1 to run", -1, nil},
		{"", 10, []string{""}},
		{"press 1 to run", 20, []string{"press 1 to run"}},
		{"press 1 to run", 8, []string{"press 1", "to run"}},
		{"press 1 to run", 1, []string{ellipsis, "1", ellipsis, ellipsis}},
		{"one\ntwo", 10, []string{"one", "two"}},
		{"one\r\ntwo", 10, []string{"one", "two"}},
		{"one\n\ntwo", 10, []string{"one", "", "two"}},
		{"  spaced   out  ", 10, []string{"spaced out"}},
		{"ключ от квартиры", 8, []string{"ключ от", "квартиры"}},
		{"日本 語", 5, []string{"日本", "語"}},
		{family + " " + family, 4, []string{family, family}},
	} {
		got := Wrap(step.text, step.width)
		if !slices.Equal(got, step.want) {
			t.Errorf("Wrap(%q, %d) = %q, want %q", step.text, step.width, got, step.want)
		}
		for _, line := range got {
			if Cells(line) > step.width {
				t.Errorf("Wrap(%q, %d) gave %q, which is %d cells wide", step.text, step.width, line, Cells(line))
			}
		}
	}
}

func TestWrapTruncatesAWordWiderThanTheColumnRatherThanBreakingIt(t *testing.T) {
	got := Wrap("run internal/judge/policy/toolgate.go now", 10)
	want := []string{"run", "internal/…", "now"}
	if !slices.Equal(got, want) {
		t.Errorf("a word wider than the column wrapped to %q, want %q", got, want)
	}
}

func TestElapsedReadsAsAClockPastAMinuteAndNeverGoesBackwards(t *testing.T) {
	for _, step := range []struct {
		since time.Duration
		want  string
	}{
		{-time.Hour, "0s"},
		{0, "0s"},
		{999 * time.Millisecond, "0s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m00s"},
		{61 * time.Second, "1m01s"},
		{time.Hour, "60m00s"},
	} {
		if got := Elapsed(step.since); got != step.want {
			t.Errorf("Elapsed(%s) = %q, want %q", step.since, got, step.want)
		}
	}
}

func TestBarFillsWhatTheFractionAsksForAndNoMore(t *testing.T) {
	for _, step := range []struct {
		fraction float64
		width    int
		want     string
	}{
		{0.5, 0, ""},
		{0.5, -2, ""},
		{0, 4, "░░░░"},
		{1, 4, "▓▓▓▓"},
		{-1, 4, "░░░░"},
		{2, 4, "▓▓▓▓"},
		{0.5, 4, "▓▓░░"},
		{0.12, 4, "░░░░"},
		{0.13, 4, "▓░░░"},
	} {
		if got := Bar(step.fraction, step.width); got != step.want {
			t.Errorf("Bar(%v, %d) = %q, want %q", step.fraction, step.width, got, step.want)
		}
	}
}

func TestSizeChangesUnitAtEachPowerOfTwoToTheTen(t *testing.T) {
	for _, step := range []struct {
		bytes int
		want  string
	}{
		{0, "0 bytes"},
		{412, "412 bytes"},
		{kilobyte - 1, "1023 bytes"},
		{kilobyte, "1.0 KB"},
		{11800, "11.5 KB"},
		{megabyte - 1, "1024.0 KB"},
		{megabyte, "1.0 MB"},
		{2202009, "2.1 MB"},
	} {
		if got := Size(step.bytes); got != step.want {
			t.Errorf("Size(%d) = %q, want %q", step.bytes, got, step.want)
		}
	}
}

func TestPercentRoundsToTheNearestWholeNumber(t *testing.T) {
	for _, step := range []struct {
		fraction float64
		want     string
	}{
		{0, "0%"},
		{0.004, "0%"},
		{0.005, "1%"},
		{0.716, "72%"},
		{1, "100%"},
	} {
		if got := Percent(step.fraction); got != step.want {
			t.Errorf("Percent(%v) = %q, want %q", step.fraction, got, step.want)
		}
	}
}
