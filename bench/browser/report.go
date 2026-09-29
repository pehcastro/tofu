package browserbench

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"tofu/bench/report"
	"tofu/bench/stat"
	"tofu/internal/browser"
	"tofu/internal/judge/ledger"
)

const (
	ultrafastMedianMillis = 178
	ultrafastInputTokens  = 5300
)

var abTasks = []struct{ id, start, goal string }{
	{"hotel", "$hotel/fixture.html", "Search Lisbon, choose the Design category, turn on Free cancellation, and open Casa Flora."},
	{"search-price", "$fixtures/shop.html", "What is the price of the Blue Kettle?"},
	{"contact-form", "$fixtures/contact.html", "Tell support, as Ada Lovelace (ada@example.com), that my kettle arrived broken. Use the damaged item topic."},
	{"contact-needs-confirmation", "$fixtures/contact.html", "Tell support, as Ada Lovelace (ada@example.com), that my kettle arrived broken. Use the damaged item topic. Do not submit without asking me."},
	{"table-extract", "$fixtures/cities.html", "Get Lyon's population."},
	{"login-wall", "$fixtures/account.html", "Show me my recent orders."},
	{"confirm-dialog", "$fixtures/drafts.html", "Delete my Holiday post draft."},
	{"wiki-open", "https://en.wikipedia.org/wiki/Main_Page", "Search for Gödel's incompleteness theorems and open that article."},
	{"pypi-open", "https://pypi.org/", "Open the project page of the httpx package."},
	{"github-open", "https://github.com/encode", "Open the httpx repository."},
	{"arxiv-open", "https://arxiv.org/", "Open the abstract page of arXiv paper 1706.03762."},
	{"hn-comments", "https://news.ycombinator.com/", "Open the comments page of the top story."},
	{"flights-search", "https://www.google.com/travel/flights", "Search for one-way nonstop flights from London to New York on the day four weeks from today."},
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func action(a browser.Action) string {
	switch a.Op {
	case browser.OpClick, browser.OpTypeText:
		return fmt.Sprintf("%s %d", a.Op, a.Element)
	case browser.OpSelect:
		return fmt.Sprintf("%s %d %q", a.Op, a.Element, a.Value)
	case browser.OpScrollUp, browser.OpScrollDown, browser.OpWait, browser.OpDone, browser.OpBlocked:
		return a.Op.String()
	}
	panic(fmt.Sprintf("browserbench: unknown op %d", int(a.Op)))
}

func Write(w io.Writer, recording Recording, readings []Reading, answersFile string) error {
	var walls, inputs, dollars []float64
	var builds []string
	correct, failed, reported := 0, 0, 0.0
	for _, r := range readings {
		if r.Error != "" {
			failed++
			continue
		}
		walls, inputs, dollars = append(walls, millis(r.Wall)), append(inputs, float64(r.Input)), append(dollars, r.Dollars())
		reported += r.Reported
		if r.Correct {
			correct++
		}
		if !slices.Contains(builds, r.Build) {
			builds = append(builds, r.Build)
		}
	}
	_, slowest := stat.Spread(walls)
	medianWall, medianInput := stat.Median(walls), stat.Median(inputs)
	conditions := recording.Conditions

	var b strings.Builder
	fmt.Fprintf(&b, "# bench browser: %s\n\n", conditions.Date)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Wire: %s, `%s`. Credential kind: %s. Jev build: %s.\n\n",
		conditions.Machine, conditions.Date, recording.Caps.Name, recording.Model, conditions.Credential, strings.Join(builds, ", "))
	b.WriteString(report.CostUnitLine([]ledger.Unit{ledger.UnitMoney, ledger.UnitListPrice}) + "\n\n")
	fmt.Fprintf(&b, "Rebuilt from `%s`, the answers recorded by one live pass on %s, with no network call. Every page was asked once. No call was discarded as a warm up. The live pass needs the network and `TOFU_LIVE=1`, and is skipped without it.\n\n", answersFile, conditions.Date)
	fmt.Fprintf(&b, "Wall time is measured around `jevloop.Jev.Choose`, request building included. Dollars are input tokens at tofu's Jev price, $%.3f per million, output free. The %s wire reported $%.6f for the whole pass.\n\n", jevDollarsPerMillionInput, recording.Caps.Name, reported)

	b.WriteString("## The chooser, tool against tool\n\n")
	b.WriteString("| arm | decisions | failed | correct | accuracy | wall p50 | wall p90 | wall max | median input tokens | dollars per decision |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
	fmt.Fprintf(&b, "| on: jev picks the op and the target | %d | %d | %d | %.0f%% | %.0f ms | %.0f ms | %.0f ms | %.0f | $%.6f |\n",
		len(readings), failed, correct, 100*float64(correct)/float64(len(readings)),
		medianWall, stat.Percentile(walls, 90), slowest, medianInput, stat.Median(dollars))
	b.WriteString("| off: the turn's model calls `browser_act` | not measured | | | | | | | | needs Chrome and a subscription turn, see the live A/B below |\n\n")
	fmt.Fprintf(&b, "Against jev-ultrafast (`docs/performance.md:28,32`): median wall %.0f ms against %d ms (%.2fx), median input tokens %.0f against %d (%.2fx). Theirs is TypeSafe direct over 17 Google Flights decisions; this is OpenRouter over %d recorded pages.\n\n",
		medianWall, ultrafastMedianMillis, medianWall/ultrafastMedianMillis, medianInput, ultrafastInputTokens, medianInput/ultrafastInputTokens, len(readings))

	b.WriteString("## Per page\n\n")
	b.WriteString("| page | want | got | correct | wall | jev latency | attempts | input | output | dollars | build | error |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range readings {
		got := "none"
		if r.Error == "" {
			got = action(r.Got)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %t | %.0f ms | %.0f ms | %d | %d | %d | $%.6f | %s | %s |\n",
			r.Case.ID, action(r.Case.Want), got, r.Correct, millis(r.Wall), millis(r.Latency), r.Attempts, r.Input, r.Output, r.Dollars(), r.Build, r.Error)
	}

	b.WriteString("\n## The live A/B, not run\n\n")
	b.WriteString("Turn against turn: 13 tasks, 3 passes per arm, on a subscription model. On: `browserDriver` goal, and the turn calls `browser_do`. Off: `browserDriver` steps, and the turn's model calls `browser_observe` and `browser_act` step by step. Per run, record success by the task's own check, actions, turn tokens and Jev tokens apart, wall time, and Jev dollars.\n\n")
	b.WriteString("Adoption check: Jev keeps the default if its success is within one task of the model arm, its median wall time is at most 0.5x the model arm's, and its turn tokens are at most 0.3x. Otherwise it is written up for him. Verdict: not evaluated, the A/B has not run.\n\n")
	b.WriteString("`$hotel` is jev-ultrafast's hotel fixture server and `$fixtures` is fastbrowse's fixture server (`src/fastbrowse/evals/local.py`), which records the POSTs the local tasks are graded on.\n\n")
	b.WriteString("```powershell\n$model = \"claude-sub/claude-opus-5\"\n$hotel = \"http://127.0.0.1:8000\"\n$fixtures = \"http://127.0.0.1:8001\"\n")
	b.WriteString("tofu settings set browser drive\ntofu settings set browserOpensTabs 1\n")
	b.WriteString("foreach ($pass in 1..3) {\n  foreach ($arm in \"goal\", \"steps\") {\n    tofu settings set browserDriver $arm\n")
	for _, task := range abTasks {
		fmt.Fprintf(&b, "    tofu run --model $model --dir (New-Item -ItemType Directory -Force \"$env:TEMP\\tofu-browser-ab\\$arm-$pass-%s\").FullName \"Open %s in a new tab, then: %s\"\n",
			task.id, task.start, strings.ReplaceAll(task.goal, "\"", "'"))
	}
	b.WriteString("  }\n}\n```\n")

	_, err := io.WriteString(w, b.String())
	return err
}
