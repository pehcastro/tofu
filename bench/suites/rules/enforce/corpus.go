package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
)

type write struct {
	rule, ticket, path, why, mark string
	before, after                 string
}

type debt struct{ rule, target, reason string }

type cut struct {
	write
	window string
	debts  []string
}

const schemaArea = "apps/desk/crates/core"

func git(root string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func readLF(file string) (string, error) {
	data, err := os.ReadFile(file)
	return strings.ReplaceAll(string(data), "\r\n", "\n"), err
}

func readPlanted(root, file string) ([]write, error) {
	data, err := readLF(file)
	if err != nil {
		return nil, err
	}
	var writes []write
	for _, record := range strings.Split(data, "=== ")[1:] {
		head, body, found := strings.Cut(record, "\n---\n")
		lines := strings.Split(head, "\n")
		fields := strings.Fields(lines[0])
		if !found || len(fields) != 3 {
			return nil, fmt.Errorf("%s: a record is a rule, a ticket and a path, then key lines, then ---: %q", file, lines[0])
		}
		w := write{rule: fields[0], ticket: fields[1], path: fields[2], after: body}
		base := "empty"
		for _, line := range lines[1:] {
			key, value, _ := strings.Cut(line, ": ")
			switch key {
			case "why":
				w.why = value
			case "mark":
				w.mark = value
			case "base":
				base = value
			default:
				return nil, fmt.Errorf("%s: %s %s has an unknown key %q", file, w.rule, w.ticket, key)
			}
		}
		switch base {
		case "empty":
		case "head":
			held, err := git(root, "show", "HEAD:"+w.path)
			if err != nil {
				return nil, err
			}
			first, rest, _ := strings.Cut(held, "\n")
			w.before, w.after = held, first+"\n"+body+rest
		default:
			return nil, fmt.Errorf("%s: %s %s has base %q, and a base is empty or head", file, w.rule, w.ticket, base)
		}
		writes = append(writes, w)
	}
	return writes, nil
}

func readDebts(file string) ([]debt, error) {
	data, err := readLF(file)
	if err != nil {
		return nil, err
	}
	var held []debt
	for _, line := range strings.Split(data, "\n") {
		if fields := strings.SplitN(line, " ", 3); len(fields) == 3 {
			held = append(held, debt{rule: fields[0], target: fields[1], reason: fields[2]})
		}
	}
	return held, nil
}

func area(r rules.Rule) string {
	switch {
	case strings.HasSuffix(r.ID, "schema_edit"):
		return schemaArea
	case strings.HasSuffix(r.ID, "static_api"):
		return "apps/desk/crates/ui"
	}
	return "apps/desk/crates"
}

func watched(r rules.Rule, path string) bool {
	r.Pattern, r.Mode = regexp.MustCompile(`(?s).+`), rules.ModeEnforced
	return rules.CheckWrite([]rules.Rule{r}, path, "", "x") != nil
}

func clean(root string, r rules.Rule, count, size int, held []debt) (taken, owed []cut, err error) {
	under := area(r)
	listed, err := git(root, "ls-tree", "-r", "--name-only", "HEAD", under)
	if err != nil {
		return nil, nil, err
	}
	var files []string
	for _, path := range strings.Fields(listed) {
		if under == schemaArea || watched(r, path) {
			files = append(files, path)
		}
	}
	slices.Sort(files)
	used := map[string]bool{}
	for i := range count {
		for at := i * len(files) / count; at < len(files); at++ {
			if used[files[at]] {
				continue
			}
			used[files[at]] = true
			c, err := window(root, r, files[at], size, held)
			if err != nil {
				return nil, nil, err
			}
			if c.debts != nil {
				owed = append(owed, c)
				continue
			}
			taken = append(taken, c)
			break
		}
	}
	return taken, owed, nil
}

func window(root string, r rules.Rule, path string, size int, held []debt) (cut, error) {
	content, err := git(root, "show", "HEAD:"+path)
	if err != nil {
		return cut{}, err
	}
	lines := strings.SplitAfter(content, "\n")
	from := max(0, len(lines)/2-size/2)
	to := min(len(lines), from+size)
	c := cut{write: write{rule: r.ID, path: path, before: strings.Join(lines[:from], "") + strings.Join(lines[to:], ""), after: content}, window: fmt.Sprintf("%s:%d-%d", path, from+1, to)}
	for _, f := range rules.CheckWrite([]rules.Rule{r}, path, c.before, c.after) {
		for _, finding := range f.Findings {
			if at := slices.IndexFunc(held, func(d debt) bool { return d.rule == r.ID && d.target == finding.Target }); at >= 0 {
				c.debts = append(c.debts, finding.Target+": "+held[at].reason)
			}
		}
	}
	return c, nil
}
