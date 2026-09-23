package shortlist

import (
	"fmt"
	"os"
	"testing"
	"time"

	"tofu/bench/report"
)

const joinCallCap = 13

func TestWriteTheJoinReport(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to write today's dated report; no network call happens here, the gate is only to keep report.Write from being asked to overwrite an existing file on a repeat run")
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		t.Skipf("no recorded sessions at %s, cannot count the join for the report: %v", sessionsDir, err)
	}
	rows, counts, err := BuildJoinCorpus(sessionsDir)
	if err != nil {
		t.Fatalf("BuildJoinCorpus: %v", err)
	}
	needed := NeededQuestions(4, 4, 1, 4)
	leaky := leakyRows(rows)
	unofferedRate := counts.UnofferedRate()
	date := time.Now().UTC().Format("2006-01-02")
	body := fmt.Sprintf(`# bench/shortlist: which denominator, and what the search join is worth

Date: %s. Machine: the owner's Windows machine, same as report-2026-09-23.md. Cost unit: dollars
that left the account behind the OpenRouter key named in .env, for Jev only, read from the response
of the call it names. Call cap stated before anything ran: %d calls, one per distinct-task hit row,
at $0.000063 a decision that is a fraction of a cent. **0 calls were made**, because the leakage
check below stopped the corpus before any arm ran.

## ANSWER

**The search join is the right denominator by argument, and it clears the count floor of 12 at 13.
It does not clear the leakage floor: 13 of 13 rows name their own label in their own task. The
corpus is not built, no arm ran, and the surviving hand-made row is untouched.**

## Which population is the right denominator

TOFU-501's population asks whether a turn changed a file that already existed: a fact about the
turn's outcome, unconnected to whether a candidate set was ever offered. TOFU-503's search join asks
for exactly the three things a shortlist question needs: a task (the turn's), a candidate set (what
search returned) and a right answer (the path the turn went on to act in). **The join is the right
denominator because it is shaped like the decision file_shortlist will make in production: rank a
candidate set search already produced.** TOFU-501's population has nothing to rank against; it is a
count of outcomes, not a count of ranking questions.

What would falsify this choice: if the acted-on path is frequently not among the offered candidates,
the "right answer" the join provides is not a right answer, only wherever the model happened to end
up, and a ranker trained or scored against it would be scored against noise rather than ground truth.

## Whether the acted-on path is a right answer or a revealed choice

**It is a revealed choice, and the unoffered rate says how often that choice disagrees with the
candidate set.** Across the 23 searches that led to an act, %d hit the offered set and %d did not:
**%.1f%% of acted-on searches ended in a path the search never returned.** For the %d rows this join
can build, the acted path is at least self-consistent with the candidate set by construction (a hit
is the only kind of row that can become a row). But the %.1f%% unoffered rate is direct evidence that
for more than a third of the turns in this same log, the model rejected every candidate the search
offered: whatever candidates a shortlist call would present, roughly a third of the time in this
history the real answer was not among them. That is a property of the candidate-generation step, not
of the join, and it bounds how good any ranker over these candidates can ever look in production
even before a single row is scored.

## How many questions the chosen population yields

Counted at run time, bench/shortlist/join_test.go, from the same %d sessions TOFU-501 and TOFU-503
read: %d search calls, %d hit, %d unoffered, %d none. Grouping hits by distinct task text, as
TOFU-503 did, yields **%d rows**, which clears the floor of %d that report-2026-09-23.md computed
from the observed 4/4-vs-1/4 gap.

## Leakage, checked before any arm ran

Same method as report-2026-09-23.md: split every path component and file stem of a row's own label
into words of three characters or more, split on non-alphanumerics and on camel case, lowercase both
sides, and look for any of them in the content words of the task. Run over all %d join rows:
**%d of %d leak.**

Every one of the 13 tasks reads "The file path/to/x_test.go is missing. Add it back, testing ...
Touch no other file," naming the missing test file whose sibling source file is the recorded label
directly, by path, in the task text. This is not incidental overlap of a common directory name: the
task states the exact stem of the answer. **These 13 rows are not file-localisation questions; they
are a single repeated maintenance template (restore a deleted test) that always names its own
target, and every instance of it leaks.** The join clears the count floor and fails the leakage floor
completely: 0 of 13 rows are usable, which is worse than TOFU-501's population, where 1 of 4 rows
survived.

## The corpus is not built, and no arm ran

Because zero rows survive leakage, testdata/shortlist-corpus.jsonl is not touched: it keeps the one
row TOFU-501 already found leak-free, item3, label src/store.ts. No BM25, grep or judged arm ran
against the join rows; the stated call cap of %d was not spent.

## The fate of the one surviving hand-made row

item3 (turn-18d6a7961d933ee8#item3, label src/store.ts) stays, on the same merits report-2026-09-23.md
already found: it is the only row in either population, hand-made or joined, that has ever passed the
leakage check. It is not joined with anything from this run because nothing from this run passed.

## What kind of evidence would clear both floors

A corpus whose tasks describe work without stating the target file's own name or stem: real
shortlist calls logged from production use, where the task is the user's own prompt and the label is
read off the outcome rather than off a task template that recites the path. A synthetic or recorded
population built from one repeated task shape, however large, will keep leaking to zero for the same
structural reason this one did.

## Verify

Machine: the owner's Windows machine, %s. Command: `+"`go test ./bench/shortlist/ -count=1`"+`, run
with TOFU_LIVE unset so every live-gated test, including this one, is skipped by default; the pasted
output is in the ticket report.
`, date, joinCallCap, counts.Hits, counts.Unoffered, 100*unofferedRate, counts.Distinct, 100*unofferedRate,
		counts.Sessions, counts.Searches, counts.Hits, counts.Unoffered, counts.None, counts.Distinct, needed,
		len(rows), len(leaky), len(rows), joinCallCap, date)

	path := "report-" + date + "-join.md"
	if err := report.Write(path, []byte(body), 0o644, "report"); err != nil {
		t.Fatalf("report.Write: %v", err)
	}
	t.Logf("wrote bench/shortlist/%s, distinct=%d needed=%d leaks=%d/%d",
		path, counts.Distinct, needed, len(leaky), len(rows))
}
