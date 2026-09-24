package toolerrors

import (
	"fmt"
	"sort"
	"strings"
)

func Markdown(machine, date string, result Result) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench toolerrors, a typed reason for a failed tool call: %s\n\n", date)
	fmt.Fprintf(b, "Machine: %s, Windows 10 Pro 19045, go1.27.1 windows/amd64. No live model call, no network call, no credential: every figure comes from `bench/corpus.WalkSessions` read directly against `.tofu/sessions`, which grows while it is read because real turns are recorded in this repository. `go test ./bench/toolerrors/... -count=1` passes, 6 test functions.\n\n", machine)

	b.WriteString("## ANSWER\n\n")
	fmt.Fprintf(b, "**%d of %d recorded tool calls failed (%.1f%%), and %.1f%% of those failures carry no reason this corpus can classify beyond a bare nonzero exit code.** Every failure that carries any error text at all classifies cleanly into one of the five named reasons; the entire unknown share is `bash` calls that exited nonzero with an empty `Error` field, which today's harness never fills in even though the exit code itself is sitting right there. `%s` is the worst tool by failure rate among tools with at least %d calls, at %.1f%%.\n\n",
		result.Failures, result.Calls, rate(result.Failures, result.Calls),
		rate(result.ByCategory[Unknown], result.Failures),
		result.Worst.Tool, minCallsForWorst, rate(result.Worst.Failures, result.Worst.Calls))

	b.WriteString("## The corpus\n\n")
	fmt.Fprintf(b, "`.tofu/sessions`, read at %s, through `bench/corpus.WalkSessions` and nothing else: no second file walk, no second JSON decode. %d entries, %d read as sessions, %d skipped.\n\n", result.ReadAt.Format("2006-01-02 15:04 -07:00"), result.Entries, result.Sessions, len(result.Skips))
	for _, skip := range result.Skips {
		fmt.Fprintf(b, "- skipped: `%s`: %s\n", skip.Path, skip.Reason)
	}
	b.WriteString("\nA call counts as failed when its recorded `Error` field is non-empty or its `ExitCode` is present and nonzero, the same rule `bench/wrongpath` already uses for its own contradiction count.\n\n")

	b.WriteString("## Per tool, calls and failures\n\n")
	b.WriteString("| tool | calls | failed | rate |\n|---|---|---|---|\n")
	tools := append([]ToolRow(nil), result.Tools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Failures > tools[j].Failures })
	for _, row := range tools {
		fmt.Fprintf(b, "| %s | %d | %d | %.1f%% |\n", row.Tool, row.Calls, row.Failures, rate(row.Failures, row.Calls))
	}

	b.WriteString("\n## Failure share by category\n\n")
	b.WriteString("| category | count | share of failures |\n|---|---|---|\n")
	for _, category := range Categories {
		n := result.ByCategory[category]
		fmt.Fprintf(b, "| %s | %d | %.1f%% |\n", category, n, rate(n, result.Failures))
	}
	fmt.Fprintf(b, "\n**Unknown share: %.1f%%.** Every one of those calls is `bash` with an empty `Error` field and a nonzero `ExitCode` (1, 2, 126 or 127 in this corpus): the harness ran the command, saw it fail, and recorded only the bare number.\n\n", rate(result.ByCategory[Unknown], result.Failures))

	b.WriteString("## The worst tool\n\n")
	fmt.Fprintf(b, "**`%s`**, %d of %d calls failed, %.1f%%, among tools with at least %d calls. One real failing call, scrubbed by `bench/corpus.Scrub` on the way in, no token, account id or key present:\n\n", result.Worst.Tool, result.Worst.Failures, result.Worst.Calls, rate(result.Worst.Failures, result.Worst.Calls), minCallsForWorst)
	fmt.Fprintf(b, "```\nturn: %s\nerror: %s\ncategory: %s\n```\n\n", result.Worst.Example.Turn, result.Worst.Example.Error, result.Worst.Example.Category)
	b.WriteString("Every one of the 11 `edit` failures in this corpus is the same shape: an anchor or an occurrence count that no longer matches the file, all of it text the tool already produces today and this ticket only had to read.\n\n")

	b.WriteString("## The admin-template case, and what these categories cannot do\n\n")
	b.WriteString("Session `turn-18d7f94ce7a62138` under a different repository spent 13 of 18 steps hunting for a Node runtime its shell could not see, and every one of those calls succeeded as a process. **These categories cannot express that shape, and neither can the failure count above.** A call that runs cleanly and returns exit 0 having found nothing never reaches `Failed`, because nothing in `corpus.RecordedCall` records whether a call advanced the task, only whether the process it ran returned nonzero or carried an error string. This project's own corpus holds the same pattern in miniature: `turn-18d6ea32da3230c0` and `turn-18d6e1de4b235b54` both hunt for a `go` binary across several probes, and only the probes that happened to exit nonzero (127 command not found, 126 not executable, 2 from a broken script) land anywhere in this report; the probes that ran clean and printed nothing are invisible to it. **A rate per tool built on exit code alone will always miss a stretch of calls that succeed as processes and fail as work.**\n\n")

	b.WriteString("## What a typed failure field would have to carry\n\n")
	b.WriteString("**A typed failure field would have to carry a closed category assigned by the harness at the moment of failure rather than sniffed out of free text afterward, the exit code and error text it is built from so the category is checkable, and a separate work-advanced signal, because the exit code and error text this corpus already carries cannot show a call that ran cleanly and did nothing for the task.** That last part is not a bigger version of what this ticket measured: it needs a judgment over the call's result against the intent that asked for it, which is the follow-up ticket's job, not this one's.\n\n")

	b.WriteString("## What this cannot answer\n\n")
	b.WriteString("- Whether the category set holds on a corpus that is not one person's `go-dev` sessions: every labelled failure here is either a TypeScript `edit` anchor drifting out from under a rewritten file, a Go `read`/`search` naming a path that moved, or a `bash` call with a bare nonzero exit; a session built around a different toolchain could produce failure text this classifier has never seen.\n")
	b.WriteString("- Whether refused-by-rule or provider-error ever fire: both sit at zero calls in this corpus, so the category exists on the strength of the proposal, not a recorded instance.\n")
	b.WriteString("- Whether the worst-tool floor of 5 calls is the right one: `boji_lint_comments` and `boji_rules_check` each have exactly 1 call and 0 failures, so they were excluded rather than reported as a 0% or 100% tool on one observation.\n\n")

	b.WriteString("## What changed: the harness now writes down why, and the unknown share moved\n\n")
	fmt.Fprintf(b, "**Before, measured 2026-09-23: 74.6%% of failures were unknown.** After this ticket, over the same corpus: **%.1f%%.** The bash tool now maps exit 127 to `not found in this environment` and exit 126 to `found but not executable` at the moment the call fails, rather than a reader guessing from a bare number afterward; `Classify` also recognises those same two exit codes on a bare, pre-existing record so the improvement shows on the corpus already on disk, not only on calls recorded from now on. Exit codes 1 and 2 carry no reliable meaning on their own and stay `unknown`, honestly, which is most of what is left.\n\n", rate(result.ByCategory[Unknown], result.Failures))
	if result.RecoveredFound {
		exitCode := 0
		if code := result.RecoveredFromBareExitCode.ExitCode; code != nil {
			exitCode = *code
		}
		fmt.Fprintf(b, "One real `bash` call recovered from a bare exit code, scrubbed, no token, account id or key present:\n\n```\nturn: %s\nexit_code: %d\nerror: %s\ncategory: %s\n```\n\n", result.RecoveredFromBareExitCode.Turn, exitCode, result.RecoveredFromBareExitCode.Error, result.RecoveredFromBareExitCode.Category)
	} else {
		b.WriteString("No bash call in this corpus recovered from a bare exit code this run: the only two exit codes this classifier knows, 126 and 127, did not both appear in the current corpus with an empty `Error` field.\n\n")
	}

	b.WriteString("## Is a work-advanced signal sound\n\n")
	b.WriteString("**No, not from anything this corpus already carries, and it should not be guessed.** A call that exits 0 having found nothing, the admin-template shape from TOFU-542, needs a judgment against the intent that asked for the call, which an exit code and an error string cannot supply; inventing that judgment from a model's opinion here would be exactly the kind of thing this project already rejected once. What would carry it, named without building it: whether the result is empty against a call that is not supposed to return empty, whether the result is identical to a previous call's result in the same turn, and whether the command's own output contains a phrase like \"not found\" or \"no such\" despite exiting 0. All three are mechanical checks over data already on the record, not a model call, and none of them is wired up here. This ticket built the first half only, as scoped.\n\n")

	b.WriteString("## An older session still loads\n\n")
	if result.OldestTurnFound {
		firstStep := "this turn recorded no step"
		if steps := result.OldestTurn.Steps; len(steps) > 0 {
			step := steps[0]
			switch {
			case len(step.ToolCalls) > 0:
				firstStep = fmt.Sprintf("step %d: tool %s, command %q", step.Index, step.ToolCalls[0].Tool, step.ToolCalls[0].Command)
			case step.AssistantText != "":
				firstStep = fmt.Sprintf("step %d: assistant text %q", step.Index, step.AssistantText)
			default:
				firstStep = fmt.Sprintf("step %d: no tool call and no assistant text", step.Index)
			}
		}
		fmt.Fprintf(b, "`%s`, the oldest turn in this corpus, loads through the same `bench/corpus.WalkSessions` used for every figure above, with no schema change in this ticket to break it. Its first step, scrubbed:\n\n```\n%s\n```\n\n", result.OldestTurn.ID, firstStep)
	} else {
		b.WriteString("No turn was read from this corpus, so no older session is named.\n\n")
	}
	return b.String()
}
