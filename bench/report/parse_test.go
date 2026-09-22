package report

import "testing"

func TestAStateIsReadOnlyFromAnEmphaticDeclarationInTheFirstLines(t *testing.T) {
	for _, one := range []struct {
		name string
		body string
		want State
	}{
		{"its own title", "# PART OF THIS REPORT IS WITHDRAWN, BOJI-133\n\nbody\n\n## Headline\n\nfound.\n", StateWithdrawnInPart},
		{"a whole file", "# THIS REPORT IS WITHDRAWN\n\n## Headline\n\nfound.\n", StateWithdrawn},
		{"a stale banner", "# stop_check\n\n> **STALE. The corpus has moved.**\n\n## Which arm won\n\nthe typed arm.\n", StateStale},
		{"prose about withdrawing", "# stop_check\n\n> Either it is regenerated or it is withdrawn, and that is the owner's call.\n\n## Which arm won\n\nthe typed arm.\n", StateStands},
		{"a later section", "# bench x\n\n## The set\n\nWITHDRAWN appears here, far past the head.\n", StateStands},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got, _ := selfDeclaredState(one.body); got != one.want {
				t.Errorf("state %q, want %q", got, one.want)
			}
		})
	}
}

func TestAConclusionIsTakenOnlyFromAHeadingThatNamesOne(t *testing.T) {
	for _, one := range []struct {
		name string
		body string
		want string
	}{
		{"a headline", "# t\n\n## Headline\n\nJev won. It cost $0.01.\n", "Jev won. It cost $0.01."},
		{"a shouted answer", "t\nTOFU-1\n\nANSWER\n\nThe measure cannot separate two arms.\n\nCONDITIONS\n\nnone\n", "The measure cannot separate two arms."},
		{"a wrapped paragraph", "# t\n\n## The answer\n\nThe volume case\ndoes not hold.\n", "The volume case does not hold."},
		{"a table under the heading", "# t\n\n## Arm that won\n\n| a | b |\n|---|---|\n\nThe free arm.\n", "The free arm."},
		{"a heading that defines rather than concludes", "# t\n\n## What the verdict columns are\n\nThey are the four questions.\n", Unparsed},
		{"no conclusion heading at all", "# t\n\n## The set\n\n17 recorded turns.\n", Unparsed},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := conclusionOf(one.body); got != one.want {
				t.Errorf("conclusion %q, want %q", got, one.want)
			}
		})
	}
}

func TestATableEarnsAChartOnlyWhenItHoldsADistribution(t *testing.T) {
	spread := Block{Kind: BlockTable, Head: []string{"cap", "calls cut"}, Rows: [][]string{
		{"100", "20"}, {"150", "18"}, {"200", "16"}, {"300", "13"}, {"500", "12"},
	}}
	chart, ok := chartFor(spread)
	if !ok || len(chart.Values) != 5 || chart.Title != "calls cut" {
		t.Fatalf("a five row numeric column is a distribution: %+v %v", chart, ok)
	}
	twoNumbers := Block{Kind: BlockTable, Head: []string{"arm", "kept"}, Rows: [][]string{{"jev", "27"}, {"free", "18"}}}
	if _, ok := chartFor(twoNumbers); ok {
		t.Error("a bar chart of two numbers is worse than the two numbers")
	}
	flat := Block{Kind: BlockTable, Head: []string{"arm", "runs"}, Rows: [][]string{
		{"a", "3"}, {"b", "3"}, {"c", "3"}, {"d", "3"}, {"e", "3"},
	}}
	if _, ok := chartFor(flat); ok {
		t.Error("one value repeated five times is not a distribution")
	}
}
