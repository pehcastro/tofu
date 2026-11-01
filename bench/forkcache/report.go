package main

import (
	"fmt"
	"slices"
	"strings"
)

const (
	forkHead    = "%-18s %-22s %4s %4s %7s %7s %7s %7s %8s %8s %6s %5s %5s %5s %5s\n"
	sessionHead = "%-18s %-22s %5s %8s %5s %6s %10s %10s\n"
	summaryHead = "%-18s %5s %-21s %-21s %10s %10s %8s %-16s %9s\n"
)

const legend = `
step     the fork came after this step; before and after are the parent's last request and the sub-session's first, in tokens billed
read     cache read on the first request after the fork; written adds every cache write from the parent's last request to it
extra    what the fork's first request cost over carrying on: carrying on reads the parent's whole request and writes what its last step added
saved    what each later request saves: a tenth of the tokens the fork took out
even     requests after the fork until extra is repaid, extra over saved; lived counts the sub-session's requests
held     of the parent's last %d lookups, results still in the first request after the fork; named only by source; gone, neither
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
	fmt.Fprintf(&b, forkHead, "arm", "session", "fork", "step", "before", "after", "read", "written", "extra", "saved", "even", "lived", "held", "named", "gone")
	for _, r := range runs {
		if len(r.forks) == 0 {
			fmt.Fprintf(&b, forkHead, r.arm, r.session, "none", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-", "-")
		}
		for at, f := range r.forks {
			fmt.Fprintf(&b, forkHead, r.arm, r.session, fmt.Sprint(at+1), fmt.Sprint(f.step), fmt.Sprint(f.parentLast.total()), fmt.Sprint(f.childFirst.total()),
				fmt.Sprint(f.childFirst.read), fmt.Sprint(f.childFirst.written()+f.asides.written()), fmt.Sprintf("%.0f", f.extra()), fmt.Sprintf("%.0f", f.saved()),
				breakEven(f), fmt.Sprint(f.lived), fmt.Sprint(f.held), fmt.Sprint(f.named), fmt.Sprint(f.gone))
		}
	}
	fmt.Fprintf(&b, legend, parentLookupsWeighed)
	fmt.Fprintf(&b, "\nsessions, repeat-lookup share: of the last %d lookups, the share whose result the next request no longer holds\n", parentLookupsWeighed)
	fmt.Fprintf(&b, sessionHead, "arm", "session", "steps", "requests", "forks", "listed", "after fork", "inside")
	for _, r := range runs {
		var t tally
		t.add(r)
		fmt.Fprintf(&b, sessionHead, r.arm, r.session, fmt.Sprint(r.steps), fmt.Sprint(r.requests), fmt.Sprint(len(r.forks)), fmt.Sprint(r.listedForks),
			share(t.named+t.gone, t.weighed), share(r.insideGone, r.inside))
	}
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
