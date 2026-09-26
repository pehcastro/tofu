package look

import (
	"slices"
	"testing"
)

func TestPaintedMatchesLipgloss(t *testing.T) {
	colours := slices.Concat(sentinelRoles, syntaxSentinels, []Color{""})
	for _, text := range []string{"", " chat ", "界界 wide 👩‍💻 é", "before \x1b[1mbold\x1b[m after", "tab\there", "two\nlines", "cr\r\nlf", "lone\rreturn"} {
		for _, fg := range colours {
			if got, want := coloured(text, fg), Style(fg).Render(text); got != want {
				t.Fatalf("coloured(%q, %q) = %q, want %q", text, fg, got, want)
			}
			for _, bg := range colours {
				if got, want := Painted(text, fg, bg), Style(fg).Background(bg.value()).Render(text); got != want {
					t.Fatalf("Painted(%q, %q, %q) = %q, want %q", text, fg, bg, got, want)
				}
			}
		}
	}
}

func BenchmarkPainted(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Painted(" chat ", Background, Mint)
	}
}
