package report

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	renderedFigure = regexp.MustCompile(`data-figure="([^"]*)"`)
	jargon         = regexp.MustCompile(`(?i)\b(arms?|placements?|unanswerable|wiring)\b`)
	quoted         = regexp.MustCompile(`(?s)<blockquote>.*?</blockquote>`)
	styleOrScript  = regexp.MustCompile(`(?s)<(style|script)\b.*?</(style|script)>`)
	markup         = regexp.MustCompile(`<[^>]*>`)
	paragraph      = regexp.MustCompile(`(?s)<(p|li|footer)[^>]*>(.*?)</(p|li|footer)>`)
	namedInCode    = regexp.MustCompile(`(?s)<code>.*?</code>`)
)

const proseFloor = 60

func readerText(page string) string {
	stripped := quoted.ReplaceAllString(styleOrScript.ReplaceAllString(page, " "), " ")
	return html.UnescapeString(markup.ReplaceAllString(stripped, " "))
}

func TestEveryMeasuredDecisionIsOnThePageWithBothMethodsOrWithWhatIsMissing(t *testing.T) {
	data := built(t)
	page := Page(data)
	want := []string{"stop_check", "read_worth", "shell_sift", "page_sift", "ask", "file_shortlist", "tool_gate", "tool_gate wording"}
	if len(data.Judgments) != len(want) {
		t.Fatalf("%d decisions are rendered and %d are measured across the reports", len(data.Judgments), len(want))
	}
	seen := map[string]JudgmentRow{}
	for _, row := range data.Judgments {
		seen[row.Point] = row
	}
	for _, point := range want {
		row, named := seen[point]
		if !named {
			t.Fatalf("%s is measured in a dated report and is not a row", point)
		}
		if !strings.Contains(page, string(row.Sample)) {
			t.Errorf("%s renders no sample size", point)
		}
		if row.Compared() {
			for _, figure := range []Figure{row.Without.Reading, row.With.Reading, row.Gap, row.Multiple} {
				if !strings.Contains(page, string(figure)) {
					t.Errorf("%s does not render %q", point, figure)
				}
			}
			if !strings.HasSuffix(string(row.Multiple), "x") {
				t.Errorf("%s states the difference as %q, which is not a multiple", point, row.Multiple)
			}
			t.Logf("%s (%s): %s %s against %s %s, winner %s by %s, %s, %s, switched on: %s",
				row.Question, point, row.Without.Name, row.Without.Reading, row.With.Name, row.With.Reading,
				row.Winner, row.Gap+", "+row.Multiple, row.Sample, row.Axis, row.SwitchedOn)
			continue
		}
		if !strings.Contains(page, "Not compared.</strong> "+html.EscapeString(row.NotCompared)) {
			t.Errorf("%s is not rendered as not compared on the page", point)
		}
		t.Logf("%s (%s): not compared. %s", row.Question, point, row.NotCompared)
	}
}

func TestADecisionThatCannotNameItsMethodsIsNeverGivenANumber(t *testing.T) {
	for _, row := range built(t).Judgments {
		if row.Compared() {
			continue
		}
		for _, empty := range []Figure{row.Without.Reading, row.With.Reading, row.Gap, row.Multiple} {
			if empty != "" {
				t.Errorf("%s names no method to compare and carries the figure %q", row.Point, empty)
			}
		}
	}
	invented := Placement{
		Point: "invented", Question: "nothing a person asks",
		SampleSize: 1, SampleOf: "case", InUse: InUseNotWired,
		Axis:   Axis{Name: "accuracy", Unit: "percent", BetterWhen: BetterHigher},
		Free:   Method{Name: "a regular expression", Does: "matches a pattern", DoesFrom: "bench/report/figure.go", Percent: 50, Hits: 1, OutOf: 2, Evidence: "no such line is in any report"},
		Judged: Method{Name: "Jev", Does: "scores the state", DoesFrom: "bench/report/figure.go", Percent: 100, Hits: 2, OutOf: 2, Evidence: "nor is this one"},
		Source: "bench/ask/report-2026-09-21.md",
	}
	bodies := map[string]string{"bench/ask/report-2026-09-21.md": "a report that says none of that"}
	if err := checkPlacement(invented, filepath.Join("..", ".."), bodies); err == nil {
		t.Fatal("a percentage citing a line no report carries was accepted")
	} else {
		t.Logf("refused, as it must be: %v", err)
	}
}

func TestEveryMethodNamedOnThePageSaysWhatItDoes(t *testing.T) {
	data := built(t)
	page := Page(data)
	named := 0
	for _, row := range data.Judgments {
		if !row.Compared() {
			continue
		}
		for _, method := range []Reading{row.Without, row.With} {
			named++
			if method.Does == "" {
				t.Errorf("%s names %q and never says what it does", row.Point, method.Name)
				continue
			}
			if !strings.Contains(page, "<strong>"+html.EscapeString(method.Name)+"</strong>") {
				t.Errorf("%s: %q is not rendered as a named method", row.Point, method.Name)
			}
			if !strings.Contains(page, html.EscapeString(method.Does)) {
				t.Errorf("%s: %q is rendered with no description beside it", row.Point, method.Name)
			}
			t.Logf("%s: %s, which %s", row.Point, method.Name, method.Does)
		}
	}
	if named == 0 {
		t.Fatal("no method is named on the page, so nothing proves a description is required")
	}
	for _, stat := range data.Answers.JevCosts {
		if !strings.Contains(page, string(stat.Figure)) || !strings.Contains(page, html.EscapeString(stat.Label)) {
			t.Errorf("the page never says %s is %s, which a reader comparing Jev with a regular expression needs", stat.Label, stat.Figure)
		}
	}
}

func TestNoTextAReaderSeesUsesThisProjectsOwnJargon(t *testing.T) {
	text := readerText(Page(built(t)))
	if found := jargon.FindAllString(text, -1); len(found) > 0 {
		t.Errorf("the page says %v to a reader, and none of those words mean anything to one", found)
	}
	for _, plain := range []string{"What decides", "Winner", "By how much", "Switched on", "the cheaper method", "not wired"} {
		if !strings.Contains(text, plain) {
			t.Errorf("the page does not say %q, which is what replaced the jargon", plain)
		}
	}
	t.Logf("%d characters of reader text outside quotations, no jargon in it", len(text))
}

func TestNoParagraphOnThePageIsConnectiveProse(t *testing.T) {
	page := Page(built(t))
	found := paragraph.FindAllStringSubmatch(quoted.ReplaceAllString(page, " "), -1)
	if len(found) == 0 {
		t.Fatal("the page holds no paragraph at all, so the check proves nothing")
	}
	longest := 0
	for _, match := range found {
		text := html.UnescapeString(markup.ReplaceAllString(namedInCode.ReplaceAllString(match[2], ""), ""))
		if len(text) > longest {
			longest = len(text)
		}
		if len(text) > proseFloor && !strings.ContainsAny(text, "0123456789") {
			t.Errorf("%d characters carrying no number: %q", len(text), text)
		}
	}
	t.Logf("%d paragraphs outside quotations, longest %d characters, none over %d without a number", len(found), longest, proseFloor)
}

func TestThePageNeverSaysShadow(t *testing.T) {
	if strings.Contains(strings.ToLower(readerText(Page(built(t)))), "shadow") {
		t.Error("the page says shadow, which is not a thing a reader can act on")
	}
	for _, row := range built(t).Judgments {
		if row.SwitchedOn != string(InUseNotWired) && !strings.HasPrefix(row.SwitchedOn, "yes, since ") {
			t.Errorf("%s reads %q, and the column takes yes with a version or not wired", row.Point, row.SwitchedOn)
		}
	}
}

func TestADecisionSwitchedOnNamesTheVersionItWentInAndTheFileThatSaysSo(t *testing.T) {
	wired := Placement{
		Point: "tool_gate", Question: "should this command be allowed to run",
		Axis:       Axis{Name: "agreement with a hand label", Unit: "percent of cases", BetterWhen: BetterHigher},
		Free:       Method{Name: "a regular expression", Does: "matches a destructive command pattern", DoesFrom: "bench/cost/regex.go", Percent: 100, Hits: 1, OutOf: 1, Evidence: "regexp"},
		Judged:     Method{Name: "Jev", Does: "scores risk, approval and whether the owner asked", DoesFrom: "bench/cost/gate.go", Percent: 100, Hits: 1, OutOf: 1, Evidence: "regexp"},
		SampleSize: 1, SampleOf: "case", InUse: InUseWired, Since: "0.3.0", SinceFrom: "CHANGELOG.md",
		Source: "bench/cost/report-2026-09-18.md",
	}
	tree := filepath.Join("..", "..")
	bodies := map[string]string{"bench/cost/report-2026-09-18.md": "regexp"}
	if err := checkPlacement(wired, tree, bodies); err != nil {
		t.Fatalf("a decision switched on in a real release was refused: %v", err)
	}
	if got := switchedOn(wired); got != "yes, since 0.3.0" {
		t.Errorf("a switched on decision reads %q", got)
	}
	wired.Since = "9.9.9"
	if err := checkPlacement(wired, tree, bodies); err == nil {
		t.Error("a version that is in no release was accepted")
	} else {
		t.Logf("refused, as it must be: %v", err)
	}
	wired.Since, wired.SinceFrom = "0.3.0", ""
	if err := checkPlacement(wired, tree, bodies); err == nil {
		t.Error("a decision switched on with no file recording it was accepted")
	}
}

func TestTheStatCardsFillEveryRowEvenly(t *testing.T) {
	for _, one := range []struct {
		cards   int
		columns int
	}{{1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 5}, {6, 3}, {7, 4}, {8, 4}, {9, 3}, {10, 5}} {
		got := cardColumns(one.cards)
		if got != one.columns {
			t.Errorf("%d cards laid out in %d columns, want %d", one.cards, got, one.columns)
		}
		if orphan := one.cards % got; orphan == 1 && one.cards > got {
			t.Errorf("%d cards in %d columns leaves one alone on the last row", one.cards, got)
		}
	}
	page := Page(built(t))
	for _, columns := range regexp.MustCompile(`class="cards" style="--cols:(\d+)"`).FindAllStringSubmatch(page, -1) {
		t.Logf("a card grid of %s columns", columns[1])
	}
}

func TestAQuotationFromAReportKeepsTheReportsOwnWords(t *testing.T) {
	page := Page(built(t))
	quotations := quoted.FindAllString(page, -1)
	if len(quotations) == 0 {
		t.Fatal("no report is quoted, so nothing proves a quotation is left alone")
	}
	for _, quotation := range quotations {
		if !strings.Contains(quotation, "<cite>") {
			t.Errorf("a quotation names no file it came from: %s", quotation)
		}
	}
	t.Logf("%d quotations, every one inside a blockquote naming the report it came from", len(quotations))
}

func TestAnAxisIsDeclaredByTheBenchAndNotFixedByTheViewer(t *testing.T) {
	data := built(t)
	added := Placement{
		Point: "shell_sift", Question: "how long did the tools take",
		Axis:       Axis{Name: "tool calls per turn", Unit: "percent of the cheaper method's calls", BetterWhen: BetterLower},
		Free:       Method{Name: "regex on error lines", Does: "keeps the head and the tail", DoesFrom: "internal/sift/shellarm.go", Percent: 100, Hits: 34, OutOf: 34, Evidence: "34 shell results this harness really returned to a model"},
		Judged:     Method{Name: "Jev", Does: "scores each chunk", DoesFrom: "bench/sift/arm.go", Percent: 50, Hits: 17, OutOf: 34, Evidence: "34 shell results this harness really returned to a model"},
		SampleSize: 34, SampleOf: "recorded shell results", InUse: InUseNotWired,
		Source: "bench/sift/report-2026-09-21.md",
	}
	before := Page(data)
	if strings.Contains(before, added.Axis.Name) {
		t.Fatalf("the page already renders %q, so adding it proves nothing", added.Axis.Name)
	}
	data.Answers.Placements = append(data.Answers.Placements, added)
	data.Judgments = rowsOf(data.Answers)
	after := Page(data)
	if !strings.Contains(after, html.EscapeString(added.Axis.Sentence())) {
		t.Errorf("the bench declared %q and the page does not render it", added.Axis.Sentence())
	}
	t.Logf("axis declared by the bench and rendered: %q", added.Axis.Sentence())
}

func TestNoNumberThePageComposesIsABareCount(t *testing.T) {
	data := built(t)
	found := renderedFigure.FindAllStringSubmatch(Page(data), -1)
	if len(found) < len(data.Judgments) {
		t.Fatalf("the page composes %d figures, which is fewer than one per decision", len(found))
	}
	for _, match := range found {
		if Figure(match[1]).IsBareCount() {
			t.Errorf("%q is rendered as a bare count, which says nothing about what it is out of", match[1])
		}
	}
	t.Logf("%d figures composed by this package on the page, checked one by one", len(found))
}

func TestOnlyOneChartIsDrawnAndItAnswersTheJudgmentQuestion(t *testing.T) {
	data := built(t)
	page := Page(data)
	if charts := strings.Count(page, "<svg"); charts != 1 {
		t.Fatalf("%d charts are drawn and only one of the three questions has an answer to chart", charts)
	}
	if !strings.Contains(page, "the cheaper method against Jev on every decision where both were measured") {
		t.Error("the one chart does not say which question it answers")
	}
	for _, row := range data.Judgments {
		drawn := strings.Contains(page, `class="reading" data-figure="`+string(row.With.Reading)+`"`)
		if row.Compared() != drawn {
			t.Errorf("%s is compared=%t and drawn=%t", row.Point, row.Compared(), drawn)
		}
	}
}
