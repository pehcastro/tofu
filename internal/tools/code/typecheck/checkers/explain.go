package checkers

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/platform/fs"
	"tofu/internal/tools/contract"
)

var (
	tscProblem   = fs.LazyRegexp(`^(.+?\.(?:ts|tsx|mts|cts|vue|svelte|js))\((\d+),(\d+)\): (error TS\d+: .+)$`)
	problemPlace = fs.LazyRegexp(`^(.+?):(\d+)(?::\d+)?$`)
	namedInError = fs.LazyRegexp("['\"`]([A-Za-z_][A-Za-z0-9_]*)['\"`]|undefined: ([A-Za-z_][A-Za-z0-9_]*)")
)

const declaringWord = `^\s*(?:export\s+)?(?:pub(?:\([a-z]+\))?\s+)?(?:async\s+)?(?:func|type|var|const|fn|struct|enum|trait|def|class|interface|let|function|mod)\s+(?:\([^)]*\)\s*)?(`

type foldedProblem struct {
	first  Problem
	places []string
}

func Explain(raw, base string) contract.Result {
	var read TestRun
	for _, run := range []TestRun{GoTest(raw), RustProblems(raw), PythonProblems(raw)} {
		for _, problem := range run.Problems {
			read.addProblem(problem)
		}
	}
	for _, line := range outputLines(raw) {
		if match := tscProblem().FindStringSubmatch(line); match != nil {
			read.addProblem(Problem{At: filepath.ToSlash(match[1]) + ":" + match[2] + ":" + match[3], Message: match[4]})
		}
	}
	var folded []foldedProblem
	for _, problem := range read.Problems {
		if problem.Warning {
			continue
		}
		at := slices.IndexFunc(folded, func(fold foldedProblem) bool { return fold.first.Message == problem.Message })
		if at < 0 {
			folded, at = append(folded, foldedProblem{first: problem}), len(folded)
		}
		folded[at].places = append(folded[at].places, problem.At)
	}
	if len(folded) == 0 {
		return contract.Failure("explain: 0 errors read: the text names no path:line error in the shape go, cargo, rustc, tsc, mypy or pyright print")
	}
	lines := []string{fmt.Sprintf("explain: %d errors, %d after folding duplicates", read.errors(), len(folded))}
	shown := folded[:min(len(folded), konst.TypecheckExplainErrors)]
	declared := declarations(base, shown)
	for _, fold := range shown {
		head := fold.first.At + ": " + fold.first.Message
		if more := fold.places[1:]; len(more) > 0 {
			listed := more[:min(len(more), konst.TypecheckExplainPlaces)]
			head += fmt.Sprintf(", %d times, also at %s", len(fold.places), strings.Join(listed, ", "))
			if cut := len(more) - len(listed); cut > 0 {
				head += fmt.Sprintf(" and %d more", cut)
			}
		}
		lines = append(append(lines, "", head), codeWindow(base, fold.first.At)...)
		var missing []string
		for _, name := range errorNames(fold.first.Message) {
			if found := declared[name]; len(found) > 0 {
				lines = append(lines, "  "+name+" is declared at "+strings.Join(found, "; "))
				continue
			}
			missing = append(missing, name)
		}
		if len(missing) > 0 {
			lines = append(lines, "  no declaration under "+base+" of "+strings.Join(missing, ", "))
		}
	}
	if cut := len(folded) - len(shown); cut > 0 {
		lines = append(lines, "", fmt.Sprintf("(%d more errors not shown)", cut))
	}
	return contract.Result{Content: strings.Join(lines, "\n") + "\n"}
}

func codeWindow(base, at string) []string {
	match := problemPlace().FindStringSubmatch(at)
	if match == nil {
		return nil
	}
	path, line := match[1], atoi(match[2])
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, filepath.FromSlash(path))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return []string{"  the code is not readable: " + err.Error()}
	}
	source := outputLines(string(body))
	from := max(1, line-konst.TypecheckExplainWindowLines/2)
	var window []string
	for number := from; number < from+konst.TypecheckExplainWindowLines && number <= len(source); number++ {
		marker := " "
		if number == line {
			marker = ">"
		}
		window = append(window, fmt.Sprintf("%s %4d | %s", marker, number, source[number-1]))
	}
	return window
}

func errorNames(message string) []string {
	var names []string
	for _, match := range namedInError().FindAllStringSubmatch(message, -1) {
		if name := match[1] + match[2]; !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func declarations(base string, shown []foldedProblem) map[string][]string {
	found := map[string][]string{}
	var names []string
	for _, fold := range shown {
		names = append(names, errorNames(fold.first.Message)...)
	}
	if len(names) == 0 {
		return found
	}
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = regexp.QuoteMeta(name)
	}
	declaring := regexp.MustCompile(declaringWord + strings.Join(quoted, "|") + `)\b`)
	eachSourcePath(base, func(path string) bool {
		if !slices.Contains(append(nativeExtensions(), typescriptExtensions()...), filepath.Ext(path)) {
			return true
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return true
		}
		relative, _ := filepath.Rel(base, path)
		for number, line := range outputLines(string(body)) {
			match := declaring.FindStringSubmatch(line)
			if match == nil || len(found[match[1]]) >= konst.TypecheckExplainDeclarations {
				continue
			}
			found[match[1]] = append(found[match[1]], filepath.ToSlash(relative)+":"+strconv.Itoa(number+1)+": "+strings.TrimSpace(line))
		}
		return true
	})
	return found
}
