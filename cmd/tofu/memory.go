package main

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/memory"
)

const memoryUsage = `tofu memory [list] [--json], tofu memory add [--scope user-local|project-local|project-global|user-global] [--kind person|project|reference] [--said "<your words>"] [--replace <id>] "<statement>", tofu memory remove [--scope <scope>] <id>, each with [--dir project]`

type memoryOpts struct {
	scope              memory.Scope
	kind               memory.Kind
	dir, said, replace string
	rest               []string
}

func (opts memoryOpts) dirFlag() string {
	if opts.dir == "." {
		return ""
	}
	return " --dir " + strconv.Quote(opts.dir)
}

func parseMemoryArgs(args []string) (memoryOpts, error) {
	opts := memoryOpts{dir: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--scope" || arg == "--kind" || arg == "--said" || arg == "--replace" || arg == "--dir" {
			if i++; i >= len(args) {
				return memoryOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		var err error
		switch {
		case arg == jsonFlag:
		case arg == "--scope":
			opts.scope, err = memory.ParseScope(args[i])
		case arg == "--kind":
			opts.kind = memory.Kind(args[i])
		case arg == "--said":
			opts.said = args[i]
		case arg == "--replace":
			opts.replace = args[i]
		case arg == "--dir":
			opts.dir = args[i]
		case strings.HasPrefix(arg, "-"):
			err = fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
		}
		if err != nil {
			return memoryOpts{}, err
		}
	}
	return opts, nil
}

func memoryVerb(args []string, out, errOut io.Writer) int {
	verb := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	o := verbOutput{verb: "memory " + verb, usageLine: memoryUsage, asJSON: jsonAsked(args), out: out, errOut: errOut}
	switch verb {
	case "tree", "zoom", "recall":
		o.usageLine = memoryTreeUsage
		return memoryTreeVerb(o, verb, args)
	}
	opts, err := parseMemoryArgs(args)
	if err != nil {
		return o.usage(err)
	}
	shelves, err := memory.Open(opts.dir)
	say := func() {
		for _, notice := range shelves.Notices {
			_, _ = fmt.Fprintln(errOut, notice)
		}
		shelves.Notices = nil
	}
	say()
	defer say()
	if err != nil {
		return o.fail(err)
	}
	switch verb {
	case "list":
		return memoryList(o, shelves)
	case "add":
		return memoryAdd(o, opts, &shelves)
	case "remove":
		return memoryRemove(o, opts, &shelves)
	}
	return o.usage(fmt.Errorf("no memory verb %q", verb))
}

func memoryList(o verbOutput, shelves memory.Memory) int {
	var report host.MemoryReport
	var precedence []string
	for _, shelf := range shelves.Shelves() {
		report.Scopes = append(report.Scopes, host.MemoryShelf{Shelf: shelf, Bytes: shelf.Bytes(), Limit: shelf.Weight.ViewBytes})
		precedence = append(precedence, string(shelf.Scope))
	}
	return o.done(true, report, func(page cli.Page) []string {
		entries := strconv.Itoa(len(shelves.All())) + " entries"
		if entries == "1 entries" {
			entries = "1 entry"
		}
		lines := []string{page.Subject("Memory") + page.Label(" · "+entries+" · where two disagree the first wins: "+strings.Join(precedence, ", "))}
		for _, scope := range report.Scopes {
			lines = append(lines, fmt.Sprintf("  %-15s %d · %d bytes of a %d byte view · promotes to %s   %s", scope.Scope, len(scope.Entries), scope.Bytes, scope.Limit, cmp.Or(string(scope.Weight.PromotesTo), "none"), page.Path(scope.Dir)))
		}
		for _, scope := range report.Scopes {
			if len(scope.Entries) == 0 {
				continue
			}
			lines = append(lines, "", page.Subject(string(scope.Scope)))
			for _, e := range scope.Entries {
				author := "another"
				if e.Yours {
					author = "you"
				}
				lines = append(lines, fmt.Sprintf("  %-8s %-9s %s  %-7s  %s", e.ID, e.Kind, e.At.Format(time.DateOnly), author, e.Text))
				if e.Said != "" {
					lines = append(lines, "       "+page.Label(strings.TrimSpace(strconv.Quote(e.Said)+"  "+e.Session)))
				}
			}
		}
		return lines
	})
}

func memoryAdd(o verbOutput, opts memoryOpts, shelves *memory.Memory) int {
	if len(opts.rest) != 1 {
		return o.usage(errors.New("one statement, in quotes"))
	}
	kind, scope := opts.kind, opts.scope
	if kind == "" {
		kind = memory.KindProject
		if scope == memory.Global || scope == memory.UserLocal {
			kind = memory.KindPerson
		}
	}
	if scope == "" {
		scope = kind.Scope()
	}
	old, _ := shelves.Find(scope, opts.replace)
	added, err := shelves.Add(memory.Entry{Scope: scope, Kind: kind, Text: opts.rest[0], Said: opts.said, At: time.Now(), By: memory.ByPerson}, opts.replace)
	if err != nil {
		return o.fail(err)
	}
	change, undo := changeAdded, added.Undo()+opts.dirFlag()
	if opts.replace != "" {
		change, undo = changeChanged, "tofu memory add --scope "+string(scope)+opts.dirFlag()+" --replace "+added.ID+" "+strconv.Quote(old.Text)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: change, What: "memory " + added.ID + " · " + string(added.Scope), File: added.File}}, Undo: undo})
}

func memoryRemove(o verbOutput, opts memoryOpts, shelves *memory.Memory) int {
	if len(opts.rest) != 1 {
		return o.usage(errors.New("one id, as tofu memory lists it"))
	}
	id, scope := opts.rest[0], opts.scope
	if scope == "" {
		var holding []string
		for _, e := range shelves.All() {
			if e.ID == id {
				holding = append(holding, string(e.Scope))
			}
		}
		if len(holding) != 1 {
			return o.fail(problemError{What: fmt.Sprintf("%d scopes hold %s: %s", len(holding), id, strings.Join(holding, ", ")), Hint: "tofu memory, then tofu memory remove --scope <scope> " + id})
		}
		scope = memory.Scope(holding[0])
	}
	gone, err := shelves.Remove(scope, id)
	if err != nil {
		return o.fail(problemError{What: err.Error(), Hint: "tofu memory"})
	}
	undo := "tofu memory add --scope " + string(gone.Scope) + opts.dirFlag() + " --kind " + string(gone.Kind)
	if gone.Said != "" {
		undo += " --said " + strconv.Quote(gone.Said)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "memory " + gone.ID + " · " + string(gone.Scope), File: gone.File}}, Undo: undo + " " + strconv.Quote(gone.Text)})
}
