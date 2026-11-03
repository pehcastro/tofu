package transform

import (
	"fmt"
	"slices"
	"strings"
)

func shapeName(row Row) string {
	switch {
	case !row.Expressible:
		return "not expressible"
	case row.Before == "":
		return "create, a file that did not exist"
	default:
		return "replace_range, a file that already existed"
	}
}

func Report(rows []Row, fit Estimate, turns int) string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "TRANSFORM ARMS over %d recorded turns, %d write calls\n\n", turns, len(rows))
	fmt.Fprintf(out, "Token estimate fitted on the %d recorded steps whose only tool call was a write: "+
		"tokens = %.1f + %.4f x bytes, worst residual %.0f tokens. The constant is per step and "+
		"falls out of the difference between the arms.\n\n",
		fit.Points, fit.StepOverhead, fit.TokensPerByte, fit.WorstResidual)

	out.WriteString(census(rows))

	fmt.Fprintf(out, "\n%-8s %4s %-16s %-14s %5s %6s %9s %9s %8s %8s\n",
		"turn", "step", "path", "shape", "edits", "bytes", "write-out", "typed-out", "write-in", "typed-in")
	var writeOut, typedOut, writeIn, typedIn, chargedOut, chargedIn int
	for _, row := range rows {
		shape := row.Shape
		if row.Expressible {
			chargedOut, chargedIn = chargedOut+fit.Tokens(row.TypedOut), chargedIn+fit.Tokens(row.TypedIn)
		} else {
			shape = "none"
			chargedOut, chargedIn = chargedOut+fit.Tokens(row.WriteOut), chargedIn+fit.Tokens(row.WriteIn)
		}
		fmt.Fprintf(out, "%-8s %4d %-16s %-14s %5d %6d %9d %9d %8d %8d\n",
			row.Session[len(row.Session)-6:], row.Step, row.Path, shape, row.Edits, len(row.Content),
			fit.Tokens(row.WriteOut), fit.Tokens(row.TypedOut), fit.Tokens(row.WriteIn), fit.Tokens(row.TypedIn))
		writeOut, typedOut = writeOut+fit.Tokens(row.WriteOut), typedOut+fit.Tokens(row.TypedOut)
		writeIn, typedIn = writeIn+fit.Tokens(row.WriteIn), typedIn+fit.Tokens(row.TypedIn)
	}
	fmt.Fprintf(out, "%-8s %4s %-16s %-14s %5s %6s %9d %9d %8d %8d\n",
		"total", "", "", "", "", "", writeOut, typedOut, writeIn, typedIn)

	fmt.Fprintf(out, "\nOut: the write arm spends %d tokens, the typed arm %d, a difference of %d (%+.1f%%).\n",
		writeOut, typedOut, typedOut-writeOut, percent(typedOut-writeOut, writeOut))
	fmt.Fprintf(out, "In: the write arm reads back %d tokens, the typed arm %d, a difference of %d.\n",
		writeIn, typedIn, typedIn-writeIn)
	out.WriteString("Both arms must read the file first when it already exists, so that cost sits in both " +
		"and the difference lives in the call the model emits.\n")

	list := &strings.Builder{}
	var refused int
	for _, row := range rows {
		if row.Expressible {
			continue
		}
		refused++
		fmt.Fprintf(list, "  %s step %d, %s: %s\n", row.Session, row.Step, row.Path, row.Why)
	}
	fmt.Fprintf(out, "\nThe typed set could not express %d of %d, so coverage is %.0f%%.\n",
		refused, len(rows), percent(len(rows)-refused, len(rows)))
	out.WriteString(list.String())

	verdict := "does not win"
	if chargedOut < writeOut {
		verdict = "wins"
	}
	fmt.Fprintf(out, "\nCharged, which is the reading that counts: a write the typed set refuses costs a full "+
		"write, because a model holding only typed edits has to fall back to sending the file. On that reading "+
		"the typed arm spends %d out and %d in against the write arm's %d and %d (%+.1f%% out), and the typed "+
		"arm %s.\n",
		chargedOut, chargedIn, writeOut, writeIn, percent(chargedOut-writeOut, writeOut), verdict)
	return out.String()
}

func census(rows []Row) string {
	counted := map[string]int{}
	for _, row := range rows {
		counted[shapeName(row)]++
	}
	names := make([]string, 0, len(counted))
	for name := range counted {
		names = append(names, name)
	}
	slices.Sort(names)
	out := &strings.Builder{}
	fmt.Fprintf(out, "%-44s %5s  %s\n", "edit shape in the recorded turns", "count", "covered by the typed set")
	for _, name := range names {
		covered := "yes"
		if name == "not expressible" {
			covered = "no"
		}
		fmt.Fprintf(out, "%-44s %5d  %s\n", name, counted[name], covered)
	}
	return out.String()
}

func percent(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}
