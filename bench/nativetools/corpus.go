package nativetools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type EditKind string

const (
	Ambiguous     EditKind = "ambiguous"
	NearDuplicate EditKind = "near-duplicate"
	Block         EditKind = "block"
	OneLine       EditKind = "one line"
)

var editQuota = []struct {
	kind EditKind
	take int
}{{Ambiguous, 4}, {NearDuplicate, 12}, {Block, 12}, {OneLine, 12}}

const (
	readQuota = 40
	listQuota = 20
)

type EditCase struct {
	ID, Run, Path, Old, New string
	Kind                    EditKind
	Occurrences             int
	Line                    int
}

type ReadCase struct {
	ID, Run, Path string
	Start, End    int
}

type ListCase struct {
	ID, Run, Pattern, Path string
}

type Corpus struct {
	Big, Seed, Base string
	Recordings      int
	Unparsed        int
	Calls           int
	Candidates      map[EditKind]int
	ReadCandidates  int
	ListCandidates  int
	Edits           []EditCase
	Reads           []ReadCase
	Lists           []ListCase
	Files           map[string]string
}

type recordedCall struct {
	run  string
	tool string
	args recordedArgs
}

type recordedArgs struct {
	Path    string `json:"path"`
	Old     string `json:"old_string"`
	New     string `json:"new_string"`
	Start   int    `json:"start_line"`
	End     int    `json:"end_line"`
	Pattern string `json:"pattern"`
}

var baseLine = regexp.MustCompile(`(?m)^BASE=([0-9a-f]{40})\s*$`)

func Load(big string) (Corpus, error) {
	script, err := os.ReadFile(filepath.Join(big, "run.sh"))
	if err != nil {
		return Corpus{}, err
	}
	found := baseLine.FindSubmatch(script)
	if found == nil {
		return Corpus{}, fmt.Errorf("%s/run.sh names no BASE commit", big)
	}
	corpus := Corpus{
		Big: big, Seed: filepath.Join(big, "seed"), Base: string(found[1]),
		Candidates: map[EditKind]int{}, Files: map[string]string{},
	}
	calls, err := corpus.recorded()
	if err != nil {
		return Corpus{}, err
	}
	corpus.Calls = len(calls)
	byKind := map[EditKind][]EditCase{}
	seen := map[string]bool{}
	var reads []ReadCase
	var lists []ListCase
	for _, call := range calls {
		args := call.args
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d", call.tool, corpus.relative(args.Path), args.Old, args.New, args.Pattern, args.Start, args.End)
		if seen[key] {
			continue
		}
		seen[key] = true
		switch {
		case call.tool == "edit" && args.Old != "":
			if edit, ok := corpus.editCase(call); ok {
				byKind[edit.Kind] = append(byKind[edit.Kind], edit)
			}
		case call.tool == "read" && args.Start > 0:
			if body, ok := corpus.file(args.Path); ok && args.Start <= len(lines(body)) {
				reads = append(reads, ReadCase{Run: call.run, Path: corpus.relative(args.Path), Start: args.Start, End: args.End})
			}
		case call.tool == "glob" && args.Pattern != "":
			lists = append(lists, ListCase{Run: call.run, Pattern: args.Pattern, Path: corpus.relative(args.Path)})
		}
	}
	for _, quota := range editQuota {
		corpus.Candidates[quota.kind] = len(byKind[quota.kind])
		corpus.Edits = append(corpus.Edits, spread(byKind[quota.kind], quota.take)...)
	}
	corpus.ReadCandidates, corpus.ListCandidates = len(reads), len(lists)
	corpus.Reads, corpus.Lists = spread(reads, readQuota), spread(lists, listQuota)
	for i := range corpus.Edits {
		corpus.Edits[i].ID = fmt.Sprintf("e%02d", i+1)
	}
	for i := range corpus.Reads {
		corpus.Reads[i].ID = fmt.Sprintf("r%02d", i+1)
	}
	for i := range corpus.Lists {
		corpus.Lists[i].ID = fmt.Sprintf("g%02d", i+1)
	}
	return corpus, nil
}

func (c *Corpus) recorded() ([]recordedCall, error) {
	runs, err := filepath.Glob(filepath.Join(c.Big, "runs", "*", "events.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Strings(runs)
	var calls []recordedCall
	for _, path := range runs {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		c.Recordings++
		run := filepath.Base(filepath.Dir(path))
		for _, line := range strings.Split(string(body), "\n") {
			var event struct {
				Kind string `json:"kind"`
				Body struct {
					Tool string          `json:"tool"`
					Args json.RawMessage `json:"args"`
				} `json:"body"`
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			if json.Unmarshal([]byte(line), &event) != nil {
				c.Unparsed++
				continue
			}
			if event.Kind != "tool_call" {
				continue
			}
			call := recordedCall{run: run, tool: event.Body.Tool}
			if json.Unmarshal(event.Body.Args, &call.args) != nil {
				c.Unparsed++
				continue
			}
			calls = append(calls, call)
		}
	}
	return calls, nil
}

func (c *Corpus) editCase(call recordedCall) (EditCase, bool) {
	body, ok := c.file(call.args.Path)
	if !ok {
		return EditCase{}, false
	}
	at := strings.Index(body, call.args.Old)
	if at < 0 {
		return EditCase{}, false
	}
	count := strings.Count(body, call.args.Old)
	edit := EditCase{
		Run: call.run, Path: c.relative(call.args.Path), Old: call.args.Old, New: call.args.New,
		Occurrences: count, Line: strings.Count(body[:at], "\n") + 1,
	}
	changed, _ := changedLines(edit.Old, edit.New)
	switch {
	case count > 1:
		edit.Kind = Ambiguous
	case !oneLineChange(changed):
		edit.Kind = Block
	case linesHolding(body, changed[0]) > 1:
		edit.Kind = NearDuplicate
	default:
		edit.Kind = OneLine
	}
	return edit, true
}

func (c *Corpus) relative(path string) string {
	path = filepath.ToSlash(path)
	seed := filepath.ToSlash(c.Seed) + "/"
	if len(path) >= len(seed) && strings.EqualFold(path[:len(seed)], seed) {
		path = path[len(seed):]
	}
	return strings.TrimPrefix(path, "./")
}

func (c *Corpus) file(path string) (string, bool) {
	path = c.relative(path)
	if body, ok := c.Files[path]; ok {
		return body, body != ""
	}
	out, _ := exec.Command("git", "-C", c.Seed, "show", c.Base+":"+path).Output()
	c.Files[path] = string(out)
	return string(out), len(out) > 0
}

func spread[T any](items []T, take int) []T {
	if len(items) <= take {
		return items
	}
	picked := make([]T, take)
	for i := range picked {
		picked[i] = items[i*len(items)/take]
	}
	return picked
}

func lines(body string) []string {
	return strings.Split(strings.TrimSuffix(body, "\n"), "\n")
}

func changedLines(old, new string) ([]string, []string) {
	before, after := strings.Split(old, "\n"), strings.Split(new, "\n")
	head := 0
	for head < min(len(before), len(after)) && before[head] == after[head] {
		head++
	}
	tail := 0
	for tail < min(len(before), len(after))-head && before[len(before)-1-tail] == after[len(after)-1-tail] {
		tail++
	}
	return before[head : len(before)-tail], after[head : len(after)-tail]
}

func oneLineChange(changed []string) bool {
	return len(changed) == 1 && strings.TrimSpace(changed[0]) != ""
}

func linesHolding(body, text string) int {
	held := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, text) {
			held++
		}
	}
	return held
}
