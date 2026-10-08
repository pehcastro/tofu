package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"tofu/interface/cli"
	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/memory"
	"tofu/internal/widget"
)

const memoryUsage = `tofu memory [list] [--json], tofu memory add [--global] [--kind person|project|reference] [--said "<your words>"] [--replace <id>] "<statement>", tofu memory remove [--global] <id>, each with [--dir project]`

type memoryOpts struct {
	scope   memory.Scope
	dir     string
	kind    memory.Kind
	said    string
	replace string
	json    bool
	rest    []string
}

func (opts memoryOpts) flags() string {
	flags := ""
	if opts.scope == memory.Global {
		flags += " --global"
	}
	if opts.dir != "." {
		flags += " --dir " + strconv.Quote(opts.dir)
	}
	return flags
}

func parseMemoryArgs(args []string) (memoryOpts, error) {
	opts := memoryOpts{scope: memory.Project, dir: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--kind" || arg == "--said" || arg == "--replace" || arg == "--dir" {
			if i++; i >= len(args) {
				return memoryOpts{}, fmt.Errorf("%s needs a value", arg)
			}
		}
		switch {
		case arg == "--global":
			opts.scope = memory.Global
		case arg == jsonFlag:
			opts.json = true
		case arg == "--kind":
			opts.kind = memory.Kind(args[i])
		case arg == "--said":
			opts.said = args[i]
		case arg == "--replace":
			opts.replace = args[i]
		case arg == "--dir":
			opts.dir = args[i]
		case strings.HasPrefix(arg, "-"):
			return memoryOpts{}, fmt.Errorf("unknown argument %q", arg)
		default:
			opts.rest = append(opts.rest, arg)
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
	opts, err := parseMemoryArgs(args)
	if err != nil {
		return o.usage(err)
	}
	shelves, err := memory.Open(opts.dir)
	if err != nil {
		return o.fail(err)
	}
	switch verb {
	case "list":
		return memoryList(o, shelves)
	case "add":
		return memoryAdd(o, opts, shelves)
	case "remove":
		return memoryRemove(o, opts, shelves)
	}
	return o.usage(fmt.Errorf("no memory verb %q", verb))
}

func memoryList(o verbOutput, shelves memory.Memory) int {
	both := []memory.Shelf{shelves.Global, shelves.Project}
	return o.done(true, host.MemoryReport{Scopes: []host.MemoryShelf{scopeReport(both[0]), scopeReport(both[1])}}, func(page cli.Page) []string {
		count := strconv.Itoa(len(both[0].Entries)+len(both[1].Entries)) + " entries"
		if count == "1 entries" {
			count = "1 entry"
		}
		lines := []string{page.Subject("Memory") + page.Label(" · "+count)}
		for _, shelf := range both {
			lines = append(lines, fmt.Sprintf("  %-8s %d · %d of %d bytes   %s", shelf.Scope, len(shelf.Entries), shelf.Bytes(), konst.MemoryScopeBytes, page.Path(shelf.Dir)))
		}
		for _, shelf := range both {
			if len(shelf.Entries) == 0 {
				continue
			}
			lines = append(lines, "", page.Subject(string(shelf.Scope)))
			for _, e := range shelf.Entries {
				lines = append(lines, fmt.Sprintf("  %-4s %-9s %s  %s", e.ID, e.Kind, e.At.Format(time.DateOnly), e.Text))
				if e.Said != "" {
					lines = append(lines, "       "+page.Label(strings.TrimSpace(strconv.Quote(e.Said)+"  "+e.Session)))
				}
			}
		}
		return lines
	})
}

func scopeReport(shelf memory.Shelf) host.MemoryShelf {
	return host.MemoryShelf{Shelf: shelf, Bytes: shelf.Bytes(), Limit: konst.MemoryScopeBytes}
}

func memoryAdd(o verbOutput, opts memoryOpts, shelves memory.Memory) int {
	if len(opts.rest) != 1 {
		return o.usage(errors.New("one statement, in quotes"))
	}
	kind := opts.kind
	if kind == "" {
		kind = memory.KindProject
		if opts.scope == memory.Global {
			kind = memory.KindPerson
		}
	}
	old, _ := shelves.Find(opts.scope, opts.replace)
	added, err := shelves.Add(memory.Entry{Scope: opts.scope, Kind: kind, Text: opts.rest[0], Said: opts.said, At: time.Now(), By: memory.ByPerson}, opts.replace)
	var full memory.FullError
	if errors.As(err, &full) {
		code := o.fail(problemError{What: full.Error(), Hint: "tofu memory remove" + opts.flags() + " <id>, or a shorter statement"})
		if !o.asJSON {
			page := cli.Detect(o.errOut, os.Environ())
			for _, e := range full.Shelf.Entries {
				row := fmt.Sprintf("    %-4s %4d bytes  %s", e.ID, e.Cost(), e.Text)
				_, _ = fmt.Fprintln(o.errOut, widget.Fit(row, page.Width))
			}
		}
		return code
	}
	if err != nil {
		return o.fail(err)
	}
	change, undo := changeAdded, "tofu memory remove"+opts.flags()+" "+added.ID
	if opts.replace != "" {
		change, undo = changeChanged, "tofu memory add"+opts.flags()+" --replace "+added.ID+" "+strconv.Quote(old.Text)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: change, What: "memory " + added.ID + " · " + string(added.Scope), File: added.File}}, Undo: undo})
}

func memoryRemove(o verbOutput, opts memoryOpts, shelves memory.Memory) int {
	if len(opts.rest) != 1 {
		return o.usage(errors.New("one id, as tofu memory lists it"))
	}
	gone, err := shelves.Remove(opts.scope, opts.rest[0])
	if err != nil {
		return o.fail(problemError{What: err.Error(), Hint: "tofu memory"})
	}
	undo := "tofu memory add" + opts.flags() + " --kind " + string(gone.Kind)
	if gone.Said != "" {
		undo += " --said " + strconv.Quote(gone.Said)
	}
	return o.receipt(writeReceipt{Changes: []fileChange{{Change: changeRemoved, What: "memory " + gone.ID + " · " + string(gone.Scope), File: gone.File}}, Undo: undo + " " + strconv.Quote(gone.Text)})
}
