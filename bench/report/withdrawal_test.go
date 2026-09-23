package report

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAWithdrawnReportNamingNoFallenSentenceIsNotQuotedAtAll(t *testing.T) {
	for _, state := range []State{StateWithdrawn, StateWithdrawnInPart} {
		said := quotable(datedEntry{
			State:      state,
			Conclusion: "the judgment keeps 27 of 34 needles where the free arm keeps 18.",
		})
		if len(said) != 1 {
			t.Fatalf("a %s report is quoted in %d parts: %+v", state, len(said), said)
		}
		if !strings.HasPrefix(said[0].Text, "not quoted here") || strings.Contains(said[0].Text, "27 of 34") {
			t.Fatalf("a %s report quotes itself: %q", state, said[0].Text)
		}
		t.Logf("%s renders: %s", state, said[0].Text)
	}
}

func TestAPartialWithdrawalKeepsWhatStandsAndStrikesWhatFell(t *testing.T) {
	for _, path := range []string{"bench/sift/report-2026-09-21.md", "bench/websift/report-2026-09-21.md"} {
		report := find(t, path)
		stands, fell := 0, 0
		for _, part := range report.Conclusion {
			if part.Fell {
				fell++
				continue
			}
			stands++
		}
		if fell == 0 || stands == 0 {
			t.Errorf("%s renders %d standing parts and %d struck ones", path, stands, fell)
		}
		if strings.Contains(report.Quoted(), "not quoted here") {
			t.Errorf("%s is withdrawn in part and blanks its standing half", path)
		}
		t.Logf("%s renders: %s", path, report.Quoted())
	}
}

func plant(t *testing.T, body, fell string) (map[string]withdrawal, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bench", "sift"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bench", "report"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bench", "sift", "report-2026-09-21.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := `[{"report":"bench/sift/report-2026-09-21.md","state":"withdrawn in part","why":"planted by the test","fell":[` + fell + `]}]`
	path := filepath.Join(root, "bench", "report", "withdrawals.json")
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	return readWithdrawals(path)
}

func TestAQuotationThatIsNotInTheReportFailsTheBuild(t *testing.T) {
	body := "# bench sift: 2026-09-21\n\n## Arm that won\n\nThe judgment, and it is not wired.\n"
	_, err := plant(t, body, `"a sentence this report never contained"`)
	if err == nil {
		t.Fatal("a withdrawal quoting a sentence the report does not carry was accepted, so the page would show the false sentence unmarked")
	}
	t.Logf("refused: %v", err)
}

func TestQuotingOneRowOfATableStrikesEveryRowOfIt(t *testing.T) {
	body := "# bench sift: 2026-09-21\n\n## Won\n\n| Arm | hit@1 |\n|---|---|\n| grep | 1/4 |\n| judged | 4/4 |\n\nThe cost table below stands.\n\n| Question | cost |\n|---|---|\n| item 3 | $0.000065 |\n"
	read, err := plant(t, body, `"| judged | 4/4 |"`)
	if err != nil {
		t.Fatal(err)
	}
	fell := read["bench/sift/report-2026-09-21.md"].Fell
	for _, want := range []string{"| Arm | hit@1 |", "| grep | 1/4 |", "| judged | 4/4 |"} {
		if !slices.Contains(fell, want) {
			t.Errorf("%q is a row of the struck table and is not struck: %q", want, fell)
		}
	}
	for _, stands := range []string{"| Question | cost |", "| item 3 | $0.000065 |"} {
		if slices.Contains(fell, stands) {
			t.Errorf("%q is in another table and was struck with the first one", stands)
		}
	}
	t.Logf("one quoted row struck %d lines: %q", len(fell), fell)
}

func TestARowOfAStruckTableCannotBeCitedAsEvidence(t *testing.T) {
	const source = "bench/shortlist/report-2026-09-21.md"
	body, err := os.ReadFile(filepath.Join("..", "shortlist", "report-2026-09-21.md"))
	if err != nil {
		t.Fatal(err)
	}
	standing, err := standingText(filepath.Join("..", ".."), map[string]string{source: plain(string(body))})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		"| Arm | hit@1 | hit@5 |",
		"| grep (weak free) | 1/4 (25.0%) | 3/4 (75.0%) |",
		"| BM25 (strong free) | 1/4 (25.0%) | 4/4 (100.0%) |",
		"| shortlist alone, unreranked (BM25 top 5, no judge) | 1/4 (25.0%) | 4/4 (100.0%) |",
		"| judged, Jev reranking the BM25 shortlist | 4/4 (100.0%) | 4/4 (100.0%) |",
	} {
		err := checkEvidence(flatten(row), source, standing)
		if err == nil {
			t.Errorf("a placement citing %q passes the build, and %s withdraws the whole table that row sits in", row, WithdrawalsPath)
			continue
		}
		t.Logf("refused: %v", err)
	}
}

func TestAPartialWithdrawalThatQuotesNothingIsRefused(t *testing.T) {
	_, err := plant(t, "# bench sift: 2026-09-21\n", "")
	if err == nil {
		t.Fatal("a partial withdrawal naming no fallen sentence was accepted")
	}
	t.Logf("refused: %v", err)
}
