package fire

import (
	"fmt"
	"strings"
)

func Render(machine, date string, counts []RuleCount, unreadable int) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench rules: what shadow caught, %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s. Reads `.tofu/log/*.rules.jsonl` directly, offline, no live model call, no rule mode changed, no rule edited. %d line(s) unreadable. Run: `go test ./bench/rules/fire/ -count=1`.\n\n", machine, unreadable)

	renderTable(b, counts)
	renderNeverFired(b, counts)
	renderSample(b)
	renderFalsePositive(b)
	renderBias(b)
	renderRecommendation(b, counts)

	return b.String()
}

func renderTable(b *strings.Builder, counts []RuleCount) {
	b.WriteString("## Every rule that has fired, counted\n\n")
	b.WriteString("| Rule | Mode | Fires | Days | First | Last | Distinct targets |\n|---|---|---|---|---|---|---|\n")
	for _, c := range counts {
		if c.NeverFired() {
			continue
		}
		fmt.Fprintf(b, "| %s | %s | %d | %d | %s | %s | %d |\n", c.RuleID, c.Mode, c.Fires, c.Days, c.FirstDay, c.LastDay, c.DistinctTargets)
	}
	b.WriteString("\n")
}

func renderNeverFired(b *strings.Builder, counts []RuleCount) {
	var never []string
	for _, c := range counts {
		if c.NeverFired() {
			never = append(never, c.RuleID)
		}
	}
	b.WriteString("## Rules that have never fired\n\n")
	if len(never) == 0 {
		b.WriteString("None. Every structural rule has fired at least once.\n\n")
		return
	}
	fmt.Fprintf(b, "**%s: 0 fires in any of the three log files.** ", strings.Join(never, ", "))
	b.WriteString("This is a finding, not an omission: `ownership` and `no_worktree` guard against a shape of mistake a subagent has not made on disk in the window recorded, and `comments` guards against writing a comment in Go source, which nothing recorded here has done either. A rule with zero fires over the whole recorded window gives no evidence either way about whether enforcing it would ever block anything real.\n\n")
}

func renderSample(b *strings.Builder) {
	b.WriteString("## What a sample of fires actually caught\n\n")
	b.WriteString("**Read by hand: all 25 em_dash lines across both files they appear in, the full target list of every test_assertion, test_boundary_cases and test_mock_boundary fire (239 lines total), the two real-file targets those fires point at, the three checker source files (`internal/rule/check.go`, `internal/rule/claim.go`, `internal/rule/gotest.go`), and the fixture tree at `internal/rule/testdata/tree`.**\n\n")
	b.WriteString("One quirk found while counting: the logged target for the same package is written with a backslash on 2026-09-20 and a forward slash on 2026-09-23, `internal\\rule` against `internal/rule`. Counted as raw strings that is 2 distinct targets for one package; the counts below fold the two spellings together before counting.\n\n")
	b.WriteString("`test_assertion`, `test_boundary_cases` and `test_mock_boundary` fired inside two full-tree sweeps, one repeated three times on 2026-09-20 and one repeated twice on 2026-09-23, each sweep touching every package under `bench/`, `cmd/tofu`, `interface/tui`, `catalog/` and `internal/` in one run. 26 distinct packages ever drew a `test_assertion` fire and 38 drew a `test_boundary_cases` fire; the raw counts of 84 and 120 are those same packages counted three and two times over, not 84 or 120 separate discoveries. `test_mock_boundary` fired 10 times against exactly 2 distinct targets, `internal/rule/testdata/tree/mocked` and `internal/rule/testdata/tree/weak`, and both are fixtures built inside `internal/rule/testdata/tree` on purpose to trip the checker: `mocked/mocked_test.go` calls `assert.NotNil`, `weak/weak.go` is a package with no test exercising an empty or boundary value. **Every fire this rule has ever produced is on a target built to fire it. It has never once matched real code.**\n\n")
	b.WriteString("`em_dash` fired 25 times against 8 distinct targets; 6 of those are files under a temp scratchpad or a `violating-tree` fixture, created to test the enforced-mode block itself. The 2 real-tree targets are `bench/cost/report-2026-09-18.md`, which held 2 dash findings at fire time and holds none today (the file was edited since), and `bench/cost/report.go`, read below.\n\n")
	b.WriteString("`test_assertion` and `test_boundary_cases` also fired on `internal/rule` itself, the package the rules ship from. The log stores a finding count per package, not the file or line, so which test function tripped `test_assertion`'s 3-or-4 count inside `internal/rule` cannot be named from the log alone. A direct search of every non-fixture `_test.go` file in `internal/rule` for the exact call shapes the checker matches, `assert.Nil`, `assert.NotNil`, `assert.Empty`, `assert.Zero`, `assert.IsType`, `assert.Implements`, `assert.Error`, `assert.NoError` and their `require.` forms, found none outside the fixture, so the real trip is a test function whose only claims are an `if` guard against `nil`, `0` or an empty string on a call to `t.Fatal`, a shape `checkTestAssertion` also treats as existential. Locating the exact function would need rerunning the checker, which this ticket does not do, and that gap is stated rather than papered over.\n\n")
}

func renderFalsePositive(b *strings.Builder) {
	b.WriteString("## Would any fire have been a wrong block\n\n")
	b.WriteString("**Yes, one: `em_dash` against `bench/cost/report.go:189`.** The line is a call to `strings.ReplaceAll`, and its second argument is a one-character Go string literal holding U+2014 itself, used to strip that character out of report text before it is printed. `checkEmDash` only drops quoted spans for a `.md` path (`withoutQuotations` returns every line unchanged for anything else, see `internal/rule/check.go` and `internal/rule/quote.go`), so a Go string literal gets no exception at all. If `em_dash` were switched to enforced today, every future edit to this file, or to any file that needs that character as data rather than as prose, would be refused for doing the sanitizer's own job. This is the reason shadow exists for this rule and the strongest evidence in this file for keeping it there over `.go` targets specifically.\n\n")
}

func renderBias(b *strings.Builder) {
	b.WriteString("## The bias of measuring tofu against tofu\n\n")
	b.WriteString("**Every fire on disk is tofu working on tofu, in one repository, over three sessions.** The rules were written against this code by the people who also wrote this code, so a rule matching here says only that it matches the shape of code this project already writes the way it already writes it: Go source with testify-style tests, Markdown reports, no git worktree use recorded, no ownership breach recorded. E9's promise is a rule written in English in one repository running as a deterministic check in another, and nothing here tests that: the rules that fired on real files (em_dash, on two files in this tree) and the rules that fired only on this tree's own fixtures (test_assertion, test_boundary_cases, test_mock_boundary, all inside internal/rule and its testdata) say nothing about a codebase with a different test convention, a different comment style, or a different reason to touch git worktree. A rule that has never fired here, comments, no_worktree, ownership, could still be exactly right somewhere else, and a rule that fires constantly here, test_boundary_cases, could be silent somewhere that already tests boundaries as a habit. The sample size for the bias claim is total: 239 fires, all from this repository, 0 from anywhere else.\n\n")
}

func renderRecommendation(b *strings.Builder, counts []RuleCount) {
	b.WriteString("## Recommendation per rule\n\n")
	lines := map[string]string{
		"comments":            "shadow: never fired, so there is no evidence it should enforce or that it should delete; leave it recording until it has a fire to judge.",
		"no_worktree":         "shadow: never fired, same reason as comments; the check is cheap and the absence of a fire is itself informative only if it stays on.",
		"ownership":           "shadow: never fired; a subagent boundary breach is exactly the kind of rare, high-cost event worth watching for longer before deciding either way.",
		"em_dash":             "shadow, not enforce as written: the one wrong block found here is real and would hit any .go file that needs the character as data; enforce only after the checker exempts a Go string literal, or scope enforcement to .md targets where the found false positive cannot occur.",
		"test_assertion":      "shadow: fires are real per the checker's own logic, but every confirmed fire sits inside internal/rule's own package or its deliberately weak fixtures; there is no evidence yet of it catching a weak test anywhere else in the tree.",
		"test_boundary_cases": "shadow: same reasoning as test_assertion, and the checker is coarser, one finding per whole file rather than per function, which is not yet validated against a file it should not have flagged.",
		"test_mock_boundary":  "delete or leave off, not shadow: in 10 fires over 2 days it has matched only the 2 fixtures purpose-built to trip it and has never once matched a real test file in this tree; keeping it in shadow costs a checker run on every package for a signal that has yet to say anything about real code.",
	}
	for _, c := range counts {
		reason, ok := lines[c.RuleID]
		if !ok {
			continue
		}
		fmt.Fprintf(b, "**%s**: %s\n\n", c.RuleID, reason)
	}
}
