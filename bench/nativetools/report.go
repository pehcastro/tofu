package nativetools

import (
	"fmt"
	"strings"
)

type Conditions struct {
	Machine, Date, OS, Shell, Sed, Find, SeedState string
	Cap                                            int
}

var editArms = []struct {
	name string
	of   func(EditRow) EditArm
}{
	{"tofu edit", func(r EditRow) EditArm { return r.Tofu }},
	{"sed, changed line", func(r EditRow) EditArm { return r.SedLine }},
	{"sed -z, whole old_string", func(r EditRow) EditArm { return r.SedWhole }},
	{"here-doc rewrite", func(r EditRow) EditArm { return r.HereDoc }},
}

var readArms = []struct {
	name     string
	numbered string
	of       func(ReadRow) ReadArm
}{
	{"tofu read", "no per-line number, one header line names the span and the file length", func(r ReadRow) ReadArm { return r.Tofu }},
	{"cat", "no", func(r ReadRow) ReadArm { return r.Cat }},
	{"sed -n", "no", func(r ReadRow) ReadArm { return r.Sed }},
	{"head and tail", "no", func(r ReadRow) ReadArm { return r.Head }},
}

func Render(at Conditions, corpus Corpus, edits []EditRow, reads []ReadRow, lists []ListRow) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# bench nativetools, %s\n\n", at.Date)
	fmt.Fprintf(b, "Tool against tool, no model in the loop. Machine %s, %s. Native arms run through `%s` with `%s` and `%s`. No network.\n\n", at.Machine, at.OS, at.Shell, at.Sed, at.Find)
	fmt.Fprintf(b, "Corpus: every `edit`, `read` and `glob` call a model really issued in %d recorded runs under `%s/runs`, %d tool calls read, %d lines unparsed. A case is kept when its file exists at the seed commit `%s` and, for an edit, its old_string occurs there. Files are read with `git show` at that commit and copied into a scratch directory per case per arm, so the seed is never written. Listings run over the seed in place, whose state was: %s.\n\n",
		corpus.Recordings, corpus.Big, corpus.Calls, corpus.Unparsed, corpus.Base, at.SeedState)
	fmt.Fprintf(b, "No call is discarded as a warm up. A result over %d bytes is cut by tofu's own cap, and by any harness cap like it, so a native output over it counts as needing a second call.\n\n", at.Cap)

	renderWins(b, edits, reads, lists)
	renderEdits(b, corpus, edits)
	renderReads(b, corpus, reads)
	renderLists(b, corpus, lists)
	return b.String()
}

type editTotal struct {
	outcomes       map[Outcome]int
	returned, sent int
}

func (t editTotal) safe() int { return t.outcomes[Right] + t.outcomes[RefusedWithReason] }

func (t editTotal) silentDamage() int { return t.outcomes[SilentWrong] + t.outcomes[Guessed] }

func totalEdits(rows []EditRow, of func(EditRow) EditArm) editTotal {
	total := editTotal{outcomes: map[Outcome]int{}}
	for _, row := range rows {
		got := of(row)
		total.outcomes[got.Outcome]++
		total.returned += got.Returned
		total.sent += got.Sent
	}
	return total
}

type readTotal struct {
	returned, complete, second int
}

func totalReads(rows []ReadRow, of func(ReadRow) ReadArm) readTotal {
	var total readTotal
	for _, row := range rows {
		total.returned += of(row).Returned
		total.complete += count(of(row).Complete)
		total.second += count(of(row).SecondCall)
	}
	return total
}

func renderWins(b *strings.Builder, edits []EditRow, reads []ReadRow, lists []ListRow) {
	var safe, damage, returned, sent, readBytes, second, listBytes []score
	for _, arm := range editArms {
		total := totalEdits(edits, arm.of)
		safe = append(safe, score{arm.name, total.safe()})
		damage = append(damage, score{arm.name, total.silentDamage()})
		returned = append(returned, score{arm.name, total.returned})
		sent = append(sent, score{arm.name, total.sent})
	}
	for _, arm := range readArms {
		total := totalReads(reads, arm.of)
		readBytes = append(readBytes, score{arm.name, total.returned})
		second = append(second, score{arm.name, total.second})
	}
	globBytes, findBytes, lsBytes, differ := 0, 0, 0, 0
	for _, row := range lists {
		globBytes += row.GlobBytes
		findBytes += row.FindBytes
		lsBytes += row.LsBytes
		differ += count(len(row.Differ) > 0)
	}
	listBytes = []score{{"glob", globBytes}, {"find", findBytes}, {"ls -R", lsBytes}}
	b.WriteString("## Which arm wins on what\n\n")
	fmt.Fprintf(b, "- edit, right or refused with a reason, most wins, of %d: %s\n", len(edits), leader(true, safe))
	fmt.Fprintf(b, "- edit, files changed wrongly with nothing said, fewest wins: %s\n", leader(false, damage))
	fmt.Fprintf(b, "- edit, bytes returned, fewest wins, and zero means the model cannot see what changed: %s\n", leader(false, returned))
	fmt.Fprintf(b, "- edit, bytes the model sends, fewest wins: %s\n", leader(false, sent))
	fmt.Fprintf(b, "- read, bytes returned, fewest wins: %s\n", leader(false, readBytes))
	fmt.Fprintf(b, "- read, answers that need a second call, of %d, fewest wins: %s\n", len(reads), leader(false, second))
	fmt.Fprintf(b, "- listing, bytes returned, fewest wins: %s. Glob and find return different paths on %d of %d, named under the table.\n\n", leader(false, listBytes), differ, len(lists))
}

type score struct {
	arm   string
	value int
}

func leader(higher bool, scores []score) string {
	best := scores[0].value
	for _, s := range scores {
		if higher && s.value > best || !higher && s.value < best {
			best = s.value
		}
	}
	var winners, rest []string
	for _, s := range scores {
		named := fmt.Sprintf("%s %d", s.arm, s.value)
		if s.value == best {
			winners = append(winners, named)
			continue
		}
		rest = append(rest, named)
	}
	if len(rest) == 0 {
		return "**" + strings.Join(winners, ", ") + "**, a tie"
	}
	return "**" + strings.Join(winners, ", ") + "**; then " + strings.Join(rest, ", ")
}

func renderEdits(b *strings.Builder, corpus Corpus, rows []EditRow) {
	fmt.Fprintf(b, "## 1. edit against sed and a here-doc rewrite, %d edits\n\n", len(rows))
	b.WriteString("Selection, spread evenly over recording order inside each kind:")
	for _, quota := range editQuota {
		fmt.Fprintf(b, " %s %d of %d,", quota.kind, quota.take, corpus.Candidates[quota.kind])
	}
	b.WriteString(" recorded edits valid at the seed.\n\n")
	b.WriteString("Kinds: *ambiguous* means old_string occurs more than once, so the only right answer is no change and a reason. *near-duplicate* means one line changes and its text is held by more than one line of the file. *block* means the change is an insertion, a deletion or several lines. *one line* means one line changes and no other line holds its text. *wrong* means the file differs from the one the call asked for: on a rename, a line sed changed beyond the one asked for may be one the model meant to change next, and the report does not credit that.\n\n")
	b.WriteString("Arms: *tofu edit* is `internal/turn/tools.Edit` called directly with the recorded arguments, its typecheck tail cut because the scratch copy has no tsconfig. *sed, changed line* is `sed -i 's/LINE/NEW/'` on the one line that changes, escaped in full, with no address, the way sed is used to save retyping the context; a block falls back to the next arm. *sed -z, whole old_string* is `sed -i -z 's/OLD/NEW/'` with the whole old_string, escaped in full. *here-doc rewrite* is `cat > path <<'EOF'` with the whole right file. Returned is what the arm prints back; sent is what the model writes to call it.\n\n")
	b.WriteString("| case | run | kind | file:line | tofu | ret | sent | sed line | ret | sent | sed -z | ret | sent | here-doc | ret | sent |\n")
	b.WriteString("|---|---|---|---|---|--:|--:|---|--:|--:|---|--:|--:|---|--:|--:|\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %s | %s | %s:%d |", row.Case.ID, row.Case.Run, row.Case.Kind, row.Case.Path, row.Case.Line)
		for _, arm := range editArms {
			got := arm.of(row)
			fmt.Fprintf(b, " %s | %d | %d |", got.Outcome, got.Returned, got.Sent)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n| arm |")
	for _, outcome := range Outcomes {
		fmt.Fprintf(b, " %s |", outcome)
	}
	b.WriteString(" returned bytes | sent bytes |\n|---|")
	b.WriteString(strings.Repeat("--:|", len(Outcomes)+2) + "\n")
	for _, arm := range editArms {
		total := totalEdits(rows, arm.of)
		fmt.Fprintf(b, "| %s |", arm.name)
		for _, outcome := range Outcomes {
			fmt.Fprintf(b, " %d |", total.outcomes[outcome])
		}
		fmt.Fprintf(b, " %d | %d |\n", total.returned, total.sent)
	}
	b.WriteString("\nBy kind, how many each arm got right, counting a refusal with a reason as right on an ambiguous edit:\n\n| kind | cases |")
	for _, arm := range editArms {
		fmt.Fprintf(b, " %s |", arm.name)
	}
	b.WriteString("\n|---|--:|" + strings.Repeat("--:|", len(editArms)) + "\n")
	for _, quota := range editQuota {
		cases := 0
		right := make([]int, len(editArms))
		for _, row := range rows {
			if row.Case.Kind != quota.kind {
				continue
			}
			cases++
			for i, arm := range editArms {
				outcome := arm.of(row).Outcome
				if outcome == Right || quota.kind == Ambiguous && outcome == RefusedWithReason {
					right[i]++
				}
			}
		}
		fmt.Fprintf(b, "| %s | %d |", quota.kind, cases)
		for _, n := range right {
			fmt.Fprintf(b, " %d |", n)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func renderReads(b *strings.Builder, corpus Corpus, rows []ReadRow) {
	fmt.Fprintf(b, "## 2. read against cat, head and sed -n, %d reads\n\n", len(rows))
	fmt.Fprintf(b, "Selection: %d of %d recorded ranged reads valid at the seed, spread evenly over recording order. Every recorded read addressed a range; none addressed a symbol, because tofu's read takes lines and its symbols tool parses Go only, and this seed is TypeScript. A symbol read costs a search and then a range read on every arm here, so it is not a separate row.\n\n", len(rows), corpus.ReadCandidates)
	b.WriteString("Arms: *tofu read* is `internal/turn.ReadTool` with the recorded start_line and end_line. *cat* is the whole file. *sed -n* is `sed -n 'S,Ep'`. *head and tail* is `head -n E | tail -n +S`. *2nd* means the first answer lacked the range or was over the cap.\n\n")
	b.WriteString("| case | run | file | range | of | wanted | tofu | 2nd | cat | 2nd | sed -n | 2nd | head+tail | 2nd |\n")
	b.WriteString("|---|---|---|---|--:|--:|--:|---|--:|---|--:|---|--:|---|\n")
	for _, row := range rows {
		end := fmt.Sprint(row.Case.End)
		if row.Case.End <= 0 {
			end = "end"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %d-%s | %d | %d |", row.Case.ID, row.Case.Run, row.Case.Path, row.Case.Start, end, row.FileLines, row.Wanted)
		for _, arm := range readArms {
			got := arm.of(row)
			fmt.Fprintf(b, " %d | %s |", got.Returned, yes(got.SecondCall))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n| arm | returned bytes | complete | second call | lines numbered |\n|---|--:|--:|--:|---|\n")
	for _, arm := range readArms {
		total := totalReads(rows, arm.of)
		fmt.Fprintf(b, "| %s | %d | %d | %d | %s |\n", arm.name, total.returned, total.complete, total.second, arm.numbered)
	}
	b.WriteString("\n")
}

func renderLists(b *strings.Builder, corpus Corpus, rows []ListRow) {
	fmt.Fprintf(b, "## 3. glob against find and ls -R, %d listings\n\n", len(rows))
	fmt.Fprintf(b, "Selection: %d of %d distinct recorded glob patterns, spread evenly over recording order. Arms: *glob* is `internal/turn/tools.Glob` over the seed. *find* is the pattern written as a model writes it, with node_modules and .git pruned: a fixed directory prefix becomes the start, one more segment becomes `-maxdepth 1 -name`, `**/` drops the depth, one brace group becomes `-name a -o -name b`, anything else is `-path`. *ls -R* lists the fixed prefix and leaves the filtering to the model. *same* compares glob's paths with find's.\n\n", len(rows), corpus.ListCandidates)
	b.WriteString("| case | run | pattern | glob bytes | glob paths | find bytes | find paths | same | ls -R bytes |\n|---|---|---|--:|--:|--:|--:|---|--:|\n")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %s | `%s` | %d | %d | %d | %d | %s | %d |\n", row.Case.ID, row.Case.Run, row.Case.Pattern, row.GlobBytes, row.GlobPaths, row.FindBytes, row.FindPaths, yes(len(row.Differ) == 0), row.LsBytes)
	}
	b.WriteString("\nThe find each case ran, and every path only one arm returned:\n\n")
	for _, row := range rows {
		fmt.Fprintf(b, "- %s: `%s`\n", row.Case.ID, row.Find)
		for _, path := range row.Differ {
			fmt.Fprintf(b, "  - %s\n", path)
		}
	}
	b.WriteString("\n")
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func count(b bool) int {
	if b {
		return 1
	}
	return 0
}
