package sift

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"tofu/internal/sift"
)

func rtkOnPath(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rtk"); err != nil {
		t.Skip("rtk is not on PATH, so the proxy arm cannot be scored on this machine")
	}
}

func TestRtkIsTheProxyAndNotSomethingElseWearingTheName(t *testing.T) {
	rtkOnPath(t)
	version, err := exec.Command("rtk", "--version").Output()
	if err != nil {
		t.Fatalf("rtk --version: %v", err)
	}
	printed := strings.TrimSpace(string(version))
	if !strings.HasPrefix(printed, "rtk ") {
		t.Fatalf("rtk --version printed %q, which is not the token proxy", printed)
	}
	gain, err := exec.Command("rtk", "gain").Output()
	if err != nil {
		t.Fatalf("rtk gain: %v, so the binary named rtk is a different tool and every proxy figure here is void", err)
	}
	if !strings.Contains(string(gain), "Token Savings") {
		t.Fatal("rtk gain printed no savings summary, so the binary named rtk is a different tool")
	}
	t.Logf("rtk --version printed %q and rtk gain answered", printed)
}

func TestRtkFilterFollowsTheLeadingCommand(t *testing.T) {
	cases := map[string]RtkFilter{
		"git -C . log --oneline -15 && echo --- && cat go.mod": "git-log",
		"git status --short | wc -l":                           "git-status",
		"git -C . diff --stat develop...HEAD":                  "git-diff",
		"grep -rn 'Version' internal/konst/*.go":               "grep",
		"find bench cmd -type d | sort":                        "find",
		"git -C . ls-files internal | cut -d/ -f1-3":           NoRtkFilter,
		"ls .local/boji/planning":                              NoRtkFilter,
		"head -70 .local/boji/tickets/BOARD.md":                NoRtkFilter,
		"for d in open doing; do ls; done":                     NoRtkFilter,
		"":                                                     NoRtkFilter,
	}
	for command, want := range cases {
		if got := RtkFilterFor(command); got != want {
			t.Errorf("%q chose %q, want %q", command, got, want)
		}
	}
}

func TestTheRtkArmOverEveryCapturedOutput(t *testing.T) {
	rtkOnPath(t)
	rows := corpusRows(t)

	var readings []Reading
	var elapsed []time.Duration
	byFilter := map[RtkFilter]int{}
	var unfiltered []string
	for i, row := range rows {
		planted, err := Plant(row, i)
		if err != nil {
			t.Fatalf("Plant: %v", err)
		}
		run, err := RunRtk(context.Background(), row.Command, planted.Shell.Stdout)
		if err != nil {
			t.Fatalf("row %d: rtk pipe: %v", i, err)
		}
		byFilter[run.Filter]++
		if run.Filter == NoRtkFilter {
			unfiltered = append(unfiltered, fmt.Sprintf("row %d %s", i, strings.Fields(row.Command)[0]))
		} else {
			elapsed = append(elapsed, run.Elapsed)
		}
		reading := Read(row, planted, run.Output)
		readings = append(readings, reading)
		t.Logf("rtk %-10s %s", run.Filter, reading.Line())
	}

	tally(t, "rtk arm, all 34", readings)
	var filtered []Reading
	for i, reading := range readings {
		if RtkFilterFor(rows[i].Command) != NoRtkFilter {
			filtered = append(filtered, reading)
		}
	}
	tally(t, "rtk arm, the rows a filter exists for", filtered)

	names := make([]string, 0, len(byFilter))
	for filter := range byFilter {
		names = append(names, fmt.Sprintf("%s %d", filter, byFilter[filter]))
	}
	sort.Strings(names)
	t.Logf("filters chosen: %s", strings.Join(names, ", "))
	t.Logf("rows with no rtk filter: %d of %d, %s", len(unfiltered), len(rows), strings.Join(unfiltered, ", "))

	if len(elapsed) == 0 {
		t.Fatal("no row reached rtk, so the proxy was never measured")
	}
	sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
	var sum time.Duration
	for _, d := range elapsed {
		sum += d
	}
	t.Logf("rtk pipe over %d rows: mean %s, p50 %s, p95 %s, worst %s, no money",
		len(elapsed), (sum / time.Duration(len(elapsed))).Round(time.Millisecond),
		elapsed[len(elapsed)/2].Round(time.Millisecond),
		elapsed[(len(elapsed)*95)/100].Round(time.Millisecond),
		elapsed[len(elapsed)-1].Round(time.Millisecond))
}

func TestTheNeedleRuleReadsTheMessageEveryArmHandsTheModel(t *testing.T) {
	row := corpusRows(t)[0]
	planted, err := Plant(row, 0)
	if err != nil {
		t.Fatal(err)
	}
	whole := sift.JoinUnits(planted.Units)
	without := strings.ReplaceAll(whole, planted.Needle, "")
	cases := []struct {
		name    string
		message string
		want    bool
	}{
		{"the output whole", whole, true},
		{"the needle removed and nothing else", without, false},
		{"rewritten around the needle", "[rtk: 200 lines omitted]\n  " + planted.Needle + "\n", true},
		{"rewritten without it", "[rtk: 200 lines omitted]\n", false},
		{"a sibling line of its unit survives and it does not", strings.SplitAfter(planted.Units[planted.At].Text, "\n")[0], false},
	}
	for _, c := range cases {
		if got := Read(row, planted, c.message).NeedleKept; got != c.want {
			t.Errorf("%s: needle kept %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRtkPassesAnOutputItHasNoFilterForThrough(t *testing.T) {
	run, err := RunRtk(context.Background(), "head -70 BOARD.md", "one\ntwo\n")
	if err != nil {
		t.Fatal(err)
	}
	if run.Filter != NoRtkFilter || run.Output != "one\ntwo\n" || run.Elapsed != 0 {
		t.Fatalf("a command rtk has no filter for was not passed through whole: %+v", run)
	}
}
