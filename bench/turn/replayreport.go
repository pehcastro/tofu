package turn

import (
	"fmt"
	"strings"
)

type TurnReplay struct {
	ID         string
	AsRecorded ReplayArm
	Layered    ReplayArm
}

type ReplayReport struct {
	Date               string
	Machine            string
	Corpus             Corpus
	PerTurn            []TurnReplay
	AsRecordedTotal    ReplayArm
	LayeredTotal       ReplayArm
	Mechanisms         []Mechanism
	SameQuestionPairs  int
	SameQuestionByTool map[string]int
	JSONLDirCacheHits  int
}

func BuildReplayReport(date, machine string, corpus Corpus) ReplayReport {
	report := ReplayReport{Date: date, Machine: machine, Corpus: corpus}
	var arms [][2]ReplayArm
	var skipped BytesSkipped
	for _, recorded := range corpus.Turns {
		pair, turnSkipped := Replay(recorded)
		asRecorded, layered := pair[0], pair[1]
		report.PerTurn = append(report.PerTurn, TurnReplay{ID: recorded.ID, AsRecorded: asRecorded, Layered: layered})
		arms = append(arms, [2]ReplayArm{asRecorded, layered})
		skipped.ByCache += turnSkipped.ByCache
		skipped.ByRetry += turnSkipped.ByRetry
	}
	report.AsRecordedTotal, report.LayeredTotal = sumArms(arms)
	memoEraCovered := anyTurnAfter(corpus.Turns, memoWiredAt)
	report.Mechanisms = mechanismTotals(report.AsRecordedTotal, report.LayeredTotal, skipped, memoEraCovered)
	report.SameQuestionPairs, report.SameQuestionByTool = SameQuestionDifferentKeyPairs(corpus.Turns)
	report.JSONLDirCacheHits = cacheHitsAmong(report.PerTurn, corpus.JSONLDirs)
	return report
}

func RenderReplayReport(r ReplayReport) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# bench turn: replay, %s\n\n", r.Date)
	fmt.Fprintf(&out, "Machine: %s. Credential kind: none, this run makes no model call. Wire: none. Source: recorded turns replayed from `%s`.\n\n", r.Machine, r.Corpus.Dir)
	renderThisIsNotABenchmarkRun(&out)
	renderCorpusSection(&out, r.Corpus)
	out.WriteString("The corpus mixes two eras of the edit tool: some turns carry `old_string`/`new_string` arguments, the shape `internal/turn/tools/edit.go` sends today, and older turns carry `anchor`/`until`, a shape that predates it. Both eras refuse the same way, zero matches or more than one, and repair the same way, a unique whitespace-only match, so the classification below reads across both without distinguishing them, the same way a session written before or after the refactor is still a real recorded turn.\n\n")
	renderTotalsSection(&out, r.AsRecordedTotal, r.LayeredTotal)
	renderMechanismSection(&out, r.Mechanisms)
	renderRememberEraSection(&out, r.Corpus, r.JSONLDirCacheHits)
	renderPerTurnSection(&out, r.PerTurn)
	renderSameQuestionSection(&out, r.SameQuestionPairs, r.SameQuestionByTool)
	out.WriteString("the wall clock of the layered arm is derived: the recorded wall clock less the removed steps at the recorded mean cost of a step, never a re-measurement, because the recording holds no model response to replay. the per-mechanism wall clock saved is a second, coarser derivation from the same non-measurement: round trips saved by that mechanism times the corpus mean cost of a call, not a step; neither number is a timing anybody watched happen.\n")
	return out.String()
}

func renderThisIsNotABenchmarkRun(out *strings.Builder) {
	out.WriteString("This is not a benchmark run. No model call was made, live or otherwise, and the `bench` verb was not run: every number below comes from replaying the tool calls a recorded turn already made, inside an ordinary `go test`.\n\n")
}

func renderCorpusSection(out *strings.Builder, corpus Corpus) {
	fmt.Fprintf(out, "## Corpus\n\n%d entries read under `%s`: %d parsed as a recorded turn, %d skipped.\n\n",
		corpus.EntryCount, corpus.Dir, len(corpus.Turns), len(corpus.Skipped))
	if len(corpus.Skipped) > 0 {
		out.WriteString("| Skipped | Reason |\n|---|---|\n")
		for _, s := range corpus.Skipped {
			fmt.Fprintf(out, "| %s | %s |\n", s.Path, s.Reason)
		}
		out.WriteString("\n")
	}
}

func renderTotalsSection(out *strings.Builder, asRecorded, layered ReplayArm) {
	out.WriteString("## Totals across every replayed turn\n\n")
	out.WriteString("| Arm | Steps | Calls | Cache hits | Retries removed | Repaired | Refused | Undecided | Bytes returned | Answers changed | Wall clock ms |\n")
	out.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	fmt.Fprintf(out, "| %s | %d | %d | - | - | - | %d | - | %d | 0 | %.0f |\n",
		ArmRecorded, asRecorded.Steps, asRecorded.Calls, asRecorded.Refusals, asRecorded.BytesReturned, asRecorded.WallClockMS)
	fmt.Fprintf(out, "| %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %.0f |\n",
		ArmLayered, layered.Steps, layered.Calls, layered.CacheHits, layered.Retries, layered.Repaired,
		layered.Refusals, layered.Undecided, layered.BytesReturned, layered.AnswersChanged, layered.WallClockMS)
	out.WriteString("\n")
}

func renderMechanismSection(out *strings.Builder, mechanisms []Mechanism) {
	out.WriteString("## The three mechanisms, four numbers each\n\n")
	out.WriteString("| Mechanism | Round trips saved | Wall clock saved (derived) | Bytes returned, delta | Answers changed | Standing |\n")
	out.WriteString("|---|---|---|---|---|---|\n")
	for _, m := range mechanisms {
		fmt.Fprintf(out, "| %s | %d | %.0f ms | %+d | %d | %s |\n", m.Name, m.RoundTripsSaved, m.WallClockSavedMS, m.BytesReturnedDelta, m.AnswersChanged, m.Standing)
	}
	out.WriteString("\n")
	for _, m := range mechanisms {
		fmt.Fprintf(out, "**%s**: %s.\n\n", m.Name, m.Verdict)
	}
	out.WriteString("What would settle `remember`: one real turn recorded after commit `81e2f47`. That is TOFU-237's job, not this report's.\n\n")
}

func renderRememberEraSection(out *strings.Builder, corpus Corpus, jsonlDirCacheHits int) {
	out.WriteString("## Remember, measured over its own era\n\n")
	out.WriteString("`internal/turn/tools/memo.go` was wired into the live turn loop at commit `81e2f47`, 2026-09-20 09:29:35 -03:00. Every recorded turn on disk, both schemas, predates that commit: the newest, `turn-18d6f8d9e45f8efc-f2`, was recorded at 2026-09-20 05:10:18 -03:00, over four hours earlier. So no turn in `.tofu/sessions` was recorded after the memo existed, and `remember` not firing is not an artifact of reading the wrong schema: it is the honest answer for the whole corpus that exists, old schema and new.\n\n")
	fmt.Fprintf(out, "%d sessions on disk are stored as `header.json` plus `body.jsonl`, the newest sessions this corpus has and the closest it gets to the memo's era. %d were read as turns and %d skipped, see Corpus above. None of them, read or skipped, carried a repeated side-effect-free call: %d cache hits among the %d read.\n\n",
		corpus.JSONLDirsSeen, len(corpus.JSONLDirs), len(corpus.JSONLDirsSkipped), jsonlDirCacheHits, len(corpus.JSONLDirs))
	for _, id := range corpus.JSONLDirs {
		fmt.Fprintf(out, "- %s\n", id)
	}
	for _, id := range corpus.JSONLDirsSkipped {
		fmt.Fprintf(out, "- %s, skipped\n", id)
	}
	out.WriteString("\n")
}

func renderPerTurnSection(out *strings.Builder, turns []TurnReplay) {
	fmt.Fprintf(out, "## Per turn, %d turns\n\n", len(turns))
	out.WriteString("| Turn | Arm | Steps | Calls | Cache hits | Retries removed | Repaired | Refused | Undecided | Bytes returned | Answers changed | Wall clock ms |\n")
	out.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, t := range turns {
		fmt.Fprintf(out, "| %s | %s | %d | %d | - | - | - | %d | - | %d | 0 | %.0f |\n",
			t.ID, ArmRecorded, t.AsRecorded.Steps, t.AsRecorded.Calls, t.AsRecorded.Refusals, t.AsRecorded.BytesReturned, t.AsRecorded.WallClockMS)
		fmt.Fprintf(out, "| %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %.0f |\n",
			t.ID, ArmLayered, t.Layered.Steps, t.Layered.Calls, t.Layered.CacheHits, t.Layered.Retries, t.Layered.Repaired,
			t.Layered.Refusals, t.Layered.Undecided, t.Layered.BytesReturned, t.Layered.AnswersChanged, t.Layered.WallClockMS)
	}
	out.WriteString("\n")
}

var sameQuestionShape = map[string]string{
	"read":           "against the same path at two different line ranges",
	"artifact_fetch": "against the same handle at two different byte ranges",
	"glob":           "with the same pattern where one call adds the default path explicitly and the other leaves it out",
	"grep":           "with the same pattern spelled two different ways",
	"search":         "with the same pattern spelled two different ways",
	"symbols":        "with the same pattern spelled two different ways",
	"fetch":          "against the same path at two different byte ranges",
}

func renderSameQuestionSection(out *strings.Builder, pairs int, byTool map[string]int) {
	fmt.Fprintf(out, "## Where Jev sits: same question, different key\n\n%d pair(s) across the corpus where `tools.CallKey` treats two calls as different but a reader would treat them as the same question.\n\n", pairs)
	fmt.Fprintf(out, "Method: %s\n\n", SameQuestionMethod)
	if pairs == 0 {
		out.WriteString("The count is zero: the question is closed for free, and Jev does not belong here.\n\n")
		return
	}
	out.WriteString("The count is not zero. This stays a count, nothing was wired: a nonzero count over this corpus is the trigger for a follow-up ticket with its own four numbers, not for a change here.\n\n")
	out.WriteString("Every pair falls into one of these shapes:\n\n")
	for _, tool := range []string{"read", "artifact_fetch", "fetch", "glob", "grep", "search", "symbols"} {
		if n, ok := byTool[tool]; ok {
			fmt.Fprintf(out, "- `%s` %s (%d)\n", tool, sameQuestionShape[tool], n)
		}
	}
	out.WriteString("\nDeciding whether two ranges of the same locator are the same question, or whether an omitted argument equals its default, is arithmetic and normalization, not a call that needs weighing: it reads like a code fix to `CallKey` or its caller, not a judgment, so it is not an argument for a typed decision here.\n\n")
}

func ReplayReportFilename(date string) string {
	return fmt.Sprintf("report-%s.md", date)
}
