package report

import (
	"regexp"
	"strings"
	"testing"
)

func minimalPage() string {
	return Page(Data{
		Wiring: []Wiring{{
			Point:      "read_worth",
			SwitchedOn: "yes, through the cheap method",
		}},
		Judgments: []JudgmentRow{{
			Point:      "read_worth",
			SwitchedOn: "yes, through the cheap method",
		}},
	})
}

func TestALongBadgeDoesNotBecomeACircle(t *testing.T) {
	page := minimalPage()
	rule := regexp.MustCompile(`\.badge\{[^}]*border-radius:([^;}]+)`)
	matches := rule.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		t.Fatal("no .badge rule with a border-radius found in the generated page")
	}
	last := matches[len(matches)-1][1]
	if last == "9999px" {
		t.Errorf(".badge still carries the full pill radius %q, so a wrapped label still draws as a circle", last)
	}
	t.Logf(".badge border-radius rules in cascade order, last wins: %v, applied: %q", matches, last)
}

func TestTheSwitchedOnTextIsNotShortened(t *testing.T) {
	page := minimalPage()
	want := "yes, through the cheap method"
	if !strings.Contains(page, want) {
		t.Errorf("the page does not carry %q from library/decisions/methods@1.yaml, unshortened", want)
	}
}
