package main

import (
	"bufio"
	"cmp"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

type verb string

const (
	verbTofu  verb = "tofu"
	verbDrive verb = "drive"
	verbServe verb = "serve"
	verbFile  verb = "file"
	verbWrite verb = "write"
	verbGit   verb = "git"
)

type step struct {
	line  string
	verb  verb
	args  []string
	env   []string
	stdin string
}

type testCase struct {
	name  string
	from  string
	steps []step
}

type manualCase struct {
	name, why string
	steps     []string
}

type unreachable struct {
	name, why string
}

type catalog struct {
	cases       []testCase
	manual      []manualCase
	unreachable []unreachable
}

func readCatalog(data fs.FS) (catalog, error) {
	files, err := fs.Glob(data, "cases/*.cases")
	if err != nil {
		return catalog{}, err
	}
	var all catalog
	seen := map[string]string{}
	for _, file := range files {
		body, err := fs.ReadFile(data, file)
		if err != nil {
			return catalog{}, err
		}
		if err := all.parse(path.Base(file), string(body), seen); err != nil {
			return catalog{}, err
		}
	}
	return all, nil
}

func (c *catalog) parse(file, body string, seen map[string]string) error {
	lines := bufio.NewScanner(strings.NewReader(body))
	number := 0
	var block string
	for lines.Scan() {
		number++
		line := strings.TrimSpace(lines.Text())
		head, rest, _ := strings.Cut(line, " ")
		where := fmt.Sprintf("%s:%d", file, number)
		switch {
		case line == "":
			block = ""
		case block == "" && (head == "case" || head == "manual" || head == "unreachable"):
			if earlier, ok := seen[rest]; ok {
				return fmt.Errorf("%s: %q is already named at %s", where, rest, earlier)
			}
			seen[rest], block = where, head
			switch head {
			case "case":
				c.cases = append(c.cases, testCase{name: rest, from: where})
			case "manual":
				c.manual = append(c.manual, manualCase{name: rest})
			case "unreachable":
				c.unreachable = append(c.unreachable, unreachable{name: rest})
			}
		case block == "case":
			parsed, err := parseStep(line)
			if err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			last := &c.cases[len(c.cases)-1]
			last.steps = append(last.steps, parsed)
		case block == "manual" && head == "why":
			c.manual[len(c.manual)-1].why = rest
		case block == "manual" && head == "step":
			last := &c.manual[len(c.manual)-1]
			last.steps = append(last.steps, rest)
		case block == "unreachable" && head == "why":
			c.unreachable[len(c.unreachable)-1].why = rest
		default:
			return fmt.Errorf("%s: %q is not a line a %q block takes", where, line, cmp.Or(block, "top level"))
		}
	}
	return lines.Err()
}

func parseStep(line string) (step, error) {
	if written := strings.SplitN(line, " ", 3); written[0] == string(verbWrite) {
		if len(written) != 3 {
			return step{}, fmt.Errorf("write takes a destination and the text to write")
		}
		return step{line: line, verb: verbWrite, args: written[1:]}, nil
	}
	tokens, err := split(line)
	if err != nil {
		return step{}, err
	}
	parsed := step{line: line}
	for len(tokens) > 0 && strings.Contains(tokens[0], "=") && !strings.HasPrefix(tokens[0], "-") {
		parsed.env, tokens = append(parsed.env, tokens[0]), tokens[1:]
	}
	if at := slices.Index(tokens, "<"); at >= 0 {
		if at != len(tokens)-2 {
			return step{}, fmt.Errorf("< takes one data file and ends the line")
		}
		parsed.stdin, tokens = tokens[at+1], tokens[:at]
	}
	if len(tokens) == 0 {
		return step{}, fmt.Errorf("a step names a verb")
	}
	parsed.verb, parsed.args = verb(tokens[0]), tokens[1:]
	switch parsed.verb {
	case verbTofu, verbGit:
	case verbDrive, verbServe:
		if len(parsed.args) == 0 {
			return step{}, fmt.Errorf("%s names its script", parsed.verb)
		}
	case verbFile:
		if len(parsed.args) != 2 {
			return step{}, fmt.Errorf("file takes a data file and a destination")
		}
	default:
		return step{}, fmt.Errorf("unknown step %q", parsed.verb)
	}
	return parsed, nil
}

func split(line string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	quoted, started := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, started = !quoted, true
		case r == ' ' && !quoted:
			if started {
				tokens = append(tokens, current.String())
			}
			current.Reset()
			started = false
		default:
			current.WriteRune(r)
			started = true
		}
	}
	if quoted {
		return nil, fmt.Errorf("an unclosed quote")
	}
	if started {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}
