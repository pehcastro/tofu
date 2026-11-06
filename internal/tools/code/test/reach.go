package test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/history/shadow"
)

type testTarget struct {
	path string
	name string
}

func (t Test) reaching(ctx context.Context, changed []string) ([]testTarget, string) {
	const none = "symbols impact: 0 tests reach this turn's edits beyond the stem tests, because "
	started, _ := shadow.StartedIn(ctx)
	diff, err := started.Store.Git(ctx, "", append([]string{"diff", "-U0", "--no-color", "--no-ext-diff", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/", shadow.TurnRefs + "/" + started.Turn.ID, "--"}, changed...)...)
	if err != nil {
		return nil, none + "the changed lines could not be read from the shadow store: " + err.Error()
	}
	weighing, cancel := context.WithTimeout(ctx, konst.TestRunDeadlineMillis*time.Millisecond)
	defer cancel()
	lines := changedLines(diff)
	var weigh []testTarget
	for _, file := range changed {
		for _, symbol := range touched(string(t.root), file, lines[file]) {
			weigh = append(weigh, testTarget{path: file, name: symbol})
		}
	}
	var said, stemFound []string
	var targets []testTarget
	named := map[string][]string{}
	nodeTest := regexp.MustCompile("^(?:test|it)\\([\"'`](.+?)[\"'`]\\)")
	for next := 0; next < len(weigh); next++ {
		declaration := weigh[next]
		raw, _ := json.Marshal(map[string]string{"name": declaration.name, "path": declaration.path, "mode": "impact"})
		result, err := t.impact.Run(weighing, raw)
		if err != nil {
			said = append(said, fmt.Sprintf("  %s in %s: %v", declaration.name, declaration.path, err))
			continue
		}
		first, _, _ := strings.Cut(result.Content, "\n")
		said = append(said, "  "+declaration.path+": "+first)
		for _, test := range listedUnder(result.Content, "tests that ") {
			at, name, _ := strings.Cut(test, " ")
			nodeName := nodeTest.FindStringSubmatch(name)
			switch at = withoutLine(at); {
			case coveredByStem(at, changed):
				if !slices.Contains(stemFound, test) {
					stemFound = append(stemFound, test)
				}
			case path.Ext(at) == ".go":
				if dir := path.Dir(at); !slices.Contains(named[dir], name) {
					named[dir] = append(named[dir], name)
				}
			case nodeName != nil && importsNodeTest(filepath.Join(string(t.root), filepath.FromSlash(at))):
				if quoted := regexp.QuoteMeta(nodeName[1]); !slices.Contains(named[at], quoted) {
					named[at] = append(named[at], quoted)
				}
			case !slices.Contains(targets, testTarget{path: at}):
				targets = append(targets, testTarget{path: at})
			}
		}
		if path.Ext(declaration.path) == ".go" {
			continue
		}
		for _, called := range listedUnder(result.Content, "callers") {
			fields := strings.Fields(called)
			caller := testTarget{path: withoutLine(fields[1]), name: strings.Join(fields[3:], " ")}
			if fields[0] != "test" && caller.name != "the file scope" && !slices.Contains(weigh, caller) {
				weigh = append(weigh, caller)
			}
		}
	}
	if len(said) == 0 {
		return nil, none + "the changed lines of " + strings.Join(changed, ", ") + " touch no declaration it can weigh"
	}
	reached := len(targets)
	for _, at := range slices.Sorted(maps.Keys(named)) {
		reached += len(named[at])
		targets = append(targets, testTarget{path: at, name: "(" + strings.Join(named[at], "|") + ")"})
	}
	return targets, fmt.Sprintf("symbols impact: beyond the stem tests, %d more reach this turn's edits; %d it found are stem tests already\n%s", reached, len(stemFound), strings.Join(said, "\n"))
}

func changedLines(diff string) map[string][][2]int {
	hunk := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)
	ranges := map[string][][2]int{}
	file := ""
	for _, line := range strings.Split(diff, "\n") {
		if name, isFile := strings.CutPrefix(line, "+++ "); isFile {
			file = strings.TrimPrefix(name, "b/")
			continue
		}
		if match := hunk.FindStringSubmatch(line); match != nil {
			first, _ := strconv.Atoi(match[1])
			count := 1
			if match[2] != "" {
				count, _ = strconv.Atoi(match[2])
			}
			ranges[file] = append(ranges[file], [2]int{max(first, 1), first + max(count, 1) - 1})
		}
	}
	return ranges
}

func touched(root, file string, ranges [][2]int) []string {
	if regexp.MustCompile(`_test\.go$|(\.|_)(test|spec)\.[jt]sx?$|^test_.*\.py$|_test\.py$`).MatchString(path.Base(file)) {
		return nil
	}
	var hit []engine.Definition
	for _, definition := range engine.Index(root, []string{file}, func(string) bool { return true }).Definitions {
		if definition.Kind != engine.KindImport && definition.Kind != engine.KindLines && slices.ContainsFunc(ranges, func(lines [2]int) bool {
			return lines[0] <= definition.LastLine && definition.FirstLine <= lines[1]
		}) {
			hit = append(hit, definition)
		}
	}
	var symbols []string
	for _, outer := range hit {
		holdsAnother := slices.ContainsFunc(hit, func(inner engine.Definition) bool {
			return inner.FirstLine != outer.FirstLine && outer.FirstLine <= inner.FirstLine && inner.LastLine <= outer.LastLine
		})
		if !holdsAnother && !slices.Contains(symbols, outer.Symbol) {
			symbols = append(symbols, outer.Symbol)
		}
	}
	return symbols
}

func withoutLine(at string) string {
	colon := strings.LastIndex(at, ":")
	if _, lineErr := strconv.Atoi(at[colon+1:]); colon > 0 && lineErr == nil {
		return at[:colon]
	}
	return at
}

func listedUnder(content, head string) []string {
	_, after, found := strings.Cut(content, "\n"+head)
	if !found {
		return nil
	}
	_, listed, _ := strings.Cut(after, "\n")
	section, _, _ := strings.Cut(listed, "\n\n")
	var entries []string
	for _, line := range strings.Split(section, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func coveredByStem(test string, changed []string) bool {
	return slices.ContainsFunc(changed, func(file string) bool {
		stem := strings.TrimSuffix(file, path.Ext(file))
		dir, base := path.Split(stem)
		return test == file || slices.ContainsFunc([]string{stem + "_test.", stem + ".test.", stem + ".spec.", dir + "test_" + base + "."}, func(prefix string) bool {
			return strings.HasPrefix(test, prefix)
		})
	})
}
