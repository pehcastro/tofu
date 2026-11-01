package main

import (
	"fmt"
	"slices"
	"strings"

	"tofu/internal/host"
)

const (
	forkHead    = "%-18s %-22s %4s %4s %7s %7s %7s %7s %8s %8s %6s %5s %5s %5s %5s %7s %7s %5s %5s\n"
	sessionHead = "%-18s %-22s %5s %8s %5s %6s %10s %10s %5s\n"
	flipHead    = "%-18s %-22s %7s %-16s %-16s %7s %7s %7s %7s %5s %5s %8s\n"
	summaryHead = "%-18s %5s %-21s %-21s %10s %10s %8s %-16s %9s\n"
)

const legend = `
step     the fork came after this step; before and after are the parent's last request and the sub-session's first, in tokens billed
read     cache read on the first request after the fork; written adds every cache write from the parent's last request to it
extra    what the fork's first request cost over carrying on: carrying on reads the parent's whole request and writes what its last step added
saved    what each later request saves: a tenth of the tokens the fork took out
even     requests after the fork until extra is repaid, extra over saved; lived counts the sub-session's requests
held     of the parent's last %d lookups, results still in the first request after the fork; named only by source; gone, neither
shared   tokens of the longest block prefix the parent's last request and the fork's first have in common
cached   tokens of the longest prefix of the request the cache already held when it arrived, the most it could read
reach    blocks from the end of that prefix to the request's next cache breakpoint; past %d the cache cannot find it and the row reads fail
`

func share(part, whole int) string {
	if whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

func breakEven(f forkRow) string {
	if f.saved() <= 0 {
		return "never"
	}
	return fmt.Sprintf("%.1f", f.extra()/f.saved())
}

func spread(values []float64, format string) string {
	if len(values) == 0 {
		return "-"
	}
	slices.Sort(values)
	return fmt.Sprintf(format+" ["+format+", "+format+"]", values[len(values)/2], values[0], values[len(values)-1])
}

type tally struct {
	forks, weighed, named, gone, inside, insideGone, paid int
	written, read, even                                   []float64
}

func (t *tally) add(r measured) {
	t.inside, t.insideGone = t.inside+r.inside, t.insideGone+r.insideGone
	for _, f := range r.forks {
		t.forks++
		t.written = append(t.written, float64(f.childFirst.written()+f.asides.written()))
		t.read = append(t.read, float64(f.childFirst.read))
		t.weighed, t.named, t.gone = t.weighed+f.held+f.named+f.gone, t.named+f.named, t.gone+f.gone
		if f.saved() > 0 {
			t.even = append(t.even, f.extra()/f.saved())
			if float64(f.lived-1) >= f.extra()/f.saved() {
				t.paid++
			}
		}
	}
}

func report(runs []measured) string {
	var b strings.Builder
	fmt.Fprintf(&b, "forks, one row per fork, in tokens and in base input token units\n")
	fmt.Fprintf(&b, forkHead, "arm", "session", "fork", "step", "before", "after", "read", "written", "extra", "saved", "even", "lived", "held", "named", "gone",
		"shared", "cached", "reach", "check")
	for _, r := range runs {
		if len(r.forks) == 0 {
			fmt.Fprintf(&b, forkHead, r.arm, r.session, "none", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-")
		}
		for at, f := range r.forks {
			reached := f.look.columns()
			fmt.Fprintf(&b, forkHead, r.arm, r.session, fmt.Sprint(at+1), fmt.Sprint(f.step), fmt.Sprint(f.parentLast.total()), fmt.Sprint(f.childFirst.total()),
				fmt.Sprint(f.childFirst.read), fmt.Sprint(f.childFirst.written()+f.asides.written()), fmt.Sprintf("%.0f", f.extra()), fmt.Sprintf("%.0f", f.saved()),
				breakEven(f), fmt.Sprint(f.lived), fmt.Sprint(f.held), fmt.Sprint(f.named), fmt.Sprint(f.gone), fmt.Sprint(f.shared), reached[0], reached[1], reached[2])
		}
	}
	fmt.Fprintf(&b, legend, parentLookupsWeighed, lookbackBlocks)
	fmt.Fprintf(&b, "\nsessions, repeat-lookup share: of the last %d lookups, the share whose result the next request no longer holds\n", parentLookupsWeighed)
	fmt.Fprintf(&b, sessionHead, "arm", "session", "steps", "requests", "forks", "listed", "after fork", "inside", "past")
	for _, r := range runs {
		var t tally
		t.add(r)
		fmt.Fprintf(&b, sessionHead, r.arm, r.session, fmt.Sprint(r.steps), fmt.Sprint(r.requests), fmt.Sprint(len(r.forks)), fmt.Sprint(r.listedForks),
			share(t.named+t.gone, t.weighed), share(r.insideGone, r.inside), fmt.Sprint(r.past))
	}
	fmt.Fprintf(&b, "past     requests of the run whose reach is past the %d-block lookback; the run exits nonzero when any is\n", lookbackBlocks)
	fmt.Fprintf(&b, "\nsummary per arm, median [least, most] over forks\n")
	fmt.Fprintf(&b, summaryHead, "arm", "forks", "written", "read", "after fork", "not named", "inside", "break-even", "paid back")
	for _, a := range arms {
		var t tally
		for _, r := range runs {
			if r.arm == a.name {
				t.add(r)
			}
		}
		fmt.Fprintf(&b, summaryHead, a.name, fmt.Sprint(t.forks), spread(t.written, "%.0f"), spread(t.read, "%.0f"), share(t.named+t.gone, t.weighed), share(t.gone, t.weighed),
			share(t.insideGone, t.inside), spread(t.even, "%.1f"), fmt.Sprintf("%d of %d", t.paid, t.forks))
	}
	return b.String()
}

const compactHead = "%-18s %-22s %-29s %-27s %7s %7s %7s %7s %7s %7s %7s %7s %7s %5s %5s %5s\n"

const compactLegend = `
first    /compact before the session's first turn, which carries nothing; after, the same once the recorded turn has run
results  old tool results it shrank; tofu's own token estimate counts the real project path, so it is left out
before   the last request before /compact, in tokens billed, with its cache read and written
after    the first request of the next turn, with its cache read and written; asides adds what every request between them wrote
shared   tokens of the longest block prefix the two requests have in common
cached   tokens of the longest prefix of the after request the cache already held, the most it could read
reach    blocks from the end of that prefix to the after request's next cache breakpoint; past %d the cache cannot find it and the row reads fail
past     requests of the compact run whose reach is past the lookback
`

func compacted(c host.Compaction) string {
	switch {
	case c.Into != "":
		return fmt.Sprint(c.Results)
	case c.TokensBefore == 0:
		return "nothing compacted, no history"
	}
	return "nothing compacted, none old"
}

func compactReport(rows []compactRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\ncompact, one row per session: /compact through tofu serve after the recorded turn, then one more turn\n")
	fmt.Fprintf(&b, compactHead, "arm", "session", "first", "results", "before", "read", "written", "after", "read", "written", "asides", "shared", "cached", "reach",
		"check", "past")
	for _, r := range rows {
		after := []string{"-", "-", "-", "-", "-", "-", "-", "-"}
		if r.compaction.Into != "" {
			after = append([]string{fmt.Sprint(r.after.total()), fmt.Sprint(r.after.read), fmt.Sprint(r.after.written()), fmt.Sprint(r.asides.written()),
				fmt.Sprint(r.shared)}, r.look.columns()...)
		}
		fmt.Fprintf(&b, compactHead, compactArm, r.session, compacted(r.first), compacted(r.compaction), fmt.Sprint(r.before.total()), fmt.Sprint(r.before.read),
			fmt.Sprint(r.before.written()), after[0], after[1], after[2], after[3], after[4], after[5], after[6], after[7], fmt.Sprint(r.past))
	}
	return b.String() + fmt.Sprintf(compactLegend, lookbackBlocks)
}

const flipLegend = `
from     the tool_choice of the request before; to, this request's, with an absent tool_choice read as auto
shared   tokens of the longest block prefix this request and the one before have in common
messages hit when the cache read reaches past the tools and system blocks, miss when it stops at them
`

func flipReport(runs []measured, compacts []compactRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\ntool_choice, one row per request whose tool_choice differs from the request before it\n")
	fmt.Fprintf(&b, flipHead, "arm", "session", "request", "from", "to", "read", "written", "shared", "cached", "reach", "check", "messages")
	row := func(arm, session string, f flip) {
		reached, messages := f.look.columns(), "miss"
		if f.look.readBlocks > f.head {
			messages = "hit"
		}
		fmt.Fprintf(&b, flipHead, arm, session, fmt.Sprint(f.request), f.from, f.choice, fmt.Sprint(f.billed.read), fmt.Sprint(f.billed.written()), fmt.Sprint(f.shared),
			reached[0], reached[1], reached[2], messages)
	}
	for _, r := range runs {
		for _, f := range r.flips {
			row(r.arm, r.session, f)
		}
	}
	for _, c := range compacts {
		for _, f := range c.flips {
			row(compactArm, c.session, f)
		}
	}
	return b.String() + flipLegend
}
