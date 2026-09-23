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

func TestATitleIsToldFromASectionByWhatPrecedesItRatherThanByPosition(t *testing.T) {
	for _, one := range []struct {
		name string
		body string
		want string
	}{
		{
			"an unshouted title above the first shouted heading",
			"TOFU-373  does a fork keep the parent's cache\nbench/forkcache, 2026-09-22\n\n\nANSWER\n\nThe first request after a context fork READS cache and writes none.\n\nCONDITIONS\n\nCorpus 2 recorded forks\n",
			"The first request after a context fork READS cache and writes none.",
		},
		{
			"a shouted title above a shouted heading",
			"BENCH PICKER, 2026-09-22\n\nANSWER\n\nThe corpus cannot separate the three arms.\n\nCONDITIONS\n\nnone\n",
			"The corpus cannot separate the three arms.",
		},
		{
			"a markdown title that names an answer it does not give",
			"# bench sift: which arm won, 2026-09-21\n\nMachine: DESKTOP-AHUN9RO.\n\n## The set\n\n17 recorded turns.\n",
			Unparsed,
		},
		{
			"a markdown title above a section that answers",
			"# bench turn: 2026-09-21\n\nMachine: DESKTOP-AHUN9RO.\n\n## Headline\n\nThe typed arm removed 4 of 11 retries.\n",
			"The typed arm removed 4 of 11 retries.",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := conclusionOf(one.body); got != one.want {
				t.Errorf("conclusion %q, want %q", got, one.want)
			}
		})
	}
}
