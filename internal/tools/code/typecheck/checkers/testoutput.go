package checkers

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/platform/fs"
)

type TestRun struct {
	Runner          string
	Read            bool
	Listed          bool
	Passed          int
	Failed          int
	Skipped         int
	PackagesOK      int
	PackagesNoTests int
	Failures        []TestFailure
	Problems        []Problem
}

type TestFailure struct {
	Name string
	At   string
	Said []string
}

type Problem struct {
	At      string
	Message string
	Warning bool
}

var (
	goRunning      = fs.LazyRegexp(`^=== (?:RUN|CONT|PAUSE|NAME)\s+(\S+)`)
	goResult       = fs.LazyRegexp(`^\s*--- (PASS|FAIL|SKIP): (\S+)`)
	goLogged       = fs.LazyRegexp(`^\s+(\S+\.go:\d+): (.*)$`)
	goProblem      = fs.LazyRegexp(`^(?:vet(?:\.exe)?: )?(\S+\.go:\d+(?::\d+)?): (.+)$`)
	goFrame        = fs.LazyRegexp(`^\t(\S+\.go:\d+)`)
	rustResult     = fs.LazyRegexp(`^test result: \w+\. (\d+) passed; (\d+) failed; (\d+) ignored`)
	rustFailed     = fs.LazyRegexp(`^test (\S+) \.\.\. FAILED$`)
	rustCaptured   = fs.LazyRegexp(`^---- (\S+) std(?:out|err) ----$`)
	rustPanicked   = fs.LazyRegexp(`^thread '[^']*'(?: \(\d+\))? panicked at (.+:\d+:\d+):$`)
	rustDiagnostic = fs.LazyRegexp(`^(error|warning)(\[\w+\])?: (.+)$`)
	rustArrow      = fs.LazyRegexp(`^\s*--> (.+:\d+:\d+)$`)
	rustLabel      = fs.LazyRegexp(`^\s*\d*\s*\|[\s|\-]*\^+\s+(.+)$`)
	pytestCounts   = fs.LazyRegexp(`(\d+) (passed|failed|skipped|errors?|xfailed|xpassed)`)
	pytestSummary  = fs.LazyRegexp(`^=*\s*(?:\d+ \w+, )*\d+ \w+ in [\d.]+s\b|^=*\s*no tests ran in `)
	pytestBlock    = fs.LazyRegexp(`^_{3,} (.+?) _{3,}$`)
	pytestLocation = fs.LazyRegexp(`^(\S+\.py:\d+): \w+`)
	pytestShort    = fs.LazyRegexp(`^(?:FAILED|ERROR) (\S+)(?: - (.*))?$`)
	unittestRan    = fs.LazyRegexp(`^Ran (\d+) tests? in `)
	unittestTally  = fs.LazyRegexp(`(failures|errors|skipped|expected failures|unexpected successes)=(\d+)`)
	unittestBlock  = fs.LazyRegexp(`^(?:FAIL|ERROR): (\S+) \((\S+)\)`)
	pythonFrame    = fs.LazyRegexp(`^\s+File "(.+)", line (\d+), in `)
	pythonProblem  = fs.LazyRegexp(`^\s*(\S.*?\.pyi?):(\d+):(?:(\d+):?)?(?: -)? (error|warning): (.+)$`)
	pythonChecked  = fs.LazyRegexp(`^(Success: no issues found|Found \d+ errors? in|\d+ errors?, \d+ warnings?, \d+ informations?)`)
)

func outputLines(output string) []string {
	return strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
}

func (r *TestRun) addProblem(problem Problem) {
	if !slices.Contains(r.Problems, problem) {
		r.Problems = append(r.Problems, problem)
	}
}

func GoTest(output string) TestRun {
	if strings.HasPrefix(strings.TrimSpace(output), "{") {
		return goTestEvents(output)
	}
	run, said, failed, current := TestRun{}, map[string][]string{}, []string{}, ""
	for _, line := range outputLines(output) {
		if strings.HasPrefix(line, "Benchmark") {
			return TestRun{}
		}
		if match := goRunning().FindStringSubmatch(line); match != nil {
			run.Listed, current = true, match[1]
			continue
		}
		if match := goResult().FindStringSubmatch(line); match != nil {
			current = match[2]
			switch match[1] {
			case "PASS":
				run.Passed++
			case "FAIL":
				run.Failed++
				failed = append(failed, current)
			case "SKIP":
				run.Skipped++
			}
			continue
		}
		run.readGoLine(line, said, current)
	}
	run.goFailures(failed, said)
	return run
}

func (r *TestRun) readGoLine(line string, said map[string][]string, current string) {
	switch {
	case goLogged().MatchString(line) || strings.HasPrefix(line, "        ") && len(said[current]) > 0:
		said[current] = append(said[current], strings.TrimSpace(line))
	case strings.HasPrefix(line, "panic: "):
		said[current] = append(said[current], line)
	case goFrame().MatchString(line) && !strings.Contains(line, "/src/runtime/") && !strings.Contains(line, "/src/testing/"):
		if lines := said[current]; len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "panic: ") {
			said[current] = append(lines[:len(lines)-1], filepath.ToSlash(goFrame().FindStringSubmatch(line)[1])+": "+lines[len(lines)-1])
		}
	case goProblem().MatchString(line):
		match := goProblem().FindStringSubmatch(line)
		r.addProblem(Problem{At: filepath.ToSlash(match[1]), Message: match[2]})
	case strings.HasPrefix(line, "ok  \t"):
		r.PackagesOK++
	case strings.HasPrefix(line, "?   \t"):
		r.PackagesNoTests++
	}
}

func (r *TestRun) goFailures(failed []string, said map[string][]string) {
	for _, name := range failed {
		lines := said[name]
		leaf := !slices.ContainsFunc(failed, func(other string) bool { return strings.HasPrefix(other, name+"/") })
		if len(lines) == 0 && !leaf {
			continue
		}
		failure := TestFailure{Name: name}
		if len(lines) > 0 {
			if at, message, found := strings.Cut(lines[0], ": "); found && goLogged().MatchString("    "+lines[0]) {
				failure.At, lines = at, append([]string{message}, lines[1:]...)
			}
		}
		failure.Said = lines
		r.Failures = append(r.Failures, failure)
	}
}

func goTestEvents(output string) TestRun {
	var text strings.Builder
	for _, line := range outputLines(output) {
		var event struct{ Test, Output string }
		if json.Unmarshal([]byte(line), &event) != nil {
			text.WriteString(line + "\n")
			continue
		}
		if event.Test != "" && event.Output != "" && !strings.HasPrefix(event.Output, "=== ") && !goResult().MatchString(event.Output) {
			text.WriteString("=== NAME  " + event.Test + "\n")
		}
		text.WriteString(event.Output)
	}
	run := GoTest(text.String())
	run.Listed = true
	return run
}

func CargoTest(output string) TestRun {
	run, current := RustProblems(output), -1
	for _, line := range outputLines(output) {
		if match := rustResult().FindStringSubmatch(line); match != nil {
			run.Listed = true
			run.Passed += atoi(match[1])
			run.Failed += atoi(match[2])
			run.Skipped += atoi(match[3])
			continue
		}
		if match := rustFailed().FindStringSubmatch(line); match != nil {
			run.failure(match[1])
			continue
		}
		if match := rustCaptured().FindStringSubmatch(line); match != nil {
			current = run.failure(match[1])
			continue
		}
		if current < 0 {
			continue
		}
		failure := &run.Failures[current]
		match := rustPanicked().FindStringSubmatch(line)
		switch {
		case match != nil:
			failure.At = filepath.ToSlash(match[1])
		case strings.TrimSpace(line) == "" && len(failure.Said) > 0, line == "failures:", strings.HasPrefix(line, "note: run with"):
			current = -1
		case failure.At != "" && strings.TrimSpace(line) != "":
			failure.Said = append(failure.Said, strings.TrimSpace(line))
		}
	}
	return run
}

func (r *TestRun) failure(name string) int {
	at := slices.IndexFunc(r.Failures, func(failure TestFailure) bool { return failure.Name == name })
	if at < 0 {
		r.Failures, at = append(r.Failures, TestFailure{Name: name}), len(r.Failures)
	}
	return at
}

func RustProblems(output string) TestRun {
	run, open, pending, seen := TestRun{}, -1, Problem{}, map[Problem]bool{}
	for _, line := range outputLines(output) {
		if strings.HasPrefix(strings.TrimSpace(line), "Finished `") {
			run.Read = true
		}
		if match := rustDiagnostic().FindStringSubmatch(line); match != nil {
			pending, open = Problem{Message: match[1] + match[2] + ": " + match[3], Warning: match[1] == "warning"}, -1
			continue
		}
		if match := rustArrow().FindStringSubmatch(line); match != nil && pending.Message != "" {
			pending.At, open = filepath.ToSlash(match[1]), -1
			if !seen[pending] {
				seen[pending] = true
				run.Problems, open = append(run.Problems, pending), len(run.Problems)
			}
			pending = Problem{}
			continue
		}
		if match := rustLabel().FindStringSubmatch(line); match != nil && open >= 0 {
			run.Problems[open].Message += ": " + match[1]
			open = -1
		}
	}
	return run
}

func Pytest(output string) TestRun {
	run, short, current := TestRun{}, []TestFailure{}, -1
	for _, line := range outputLines(output) {
		switch {
		case pytestSummary().MatchString(line):
			run.Listed = true
			for _, count := range pytestCounts().FindAllStringSubmatch(line, -1) {
				switch count[2] {
				case "passed", "xpassed":
					run.Passed += atoi(count[1])
				case "failed", "error", "errors":
					run.Failed += atoi(count[1])
				case "skipped", "xfailed":
					run.Skipped += atoi(count[1])
				}
			}
		case pytestBlock().MatchString(line):
			name := pytestBlock().FindStringSubmatch(line)[1]
			run.Failures, current = append(run.Failures, TestFailure{Name: name}), len(run.Failures)
		case strings.HasPrefix(line, "====="):
			current = -1
		case pytestShort().MatchString(line):
			match := pytestShort().FindStringSubmatch(line)
			failure := TestFailure{Name: match[1]}
			if match[2] != "" {
				failure.Said = []string{match[2]}
			}
			short = append(short, failure)
		case current >= 0 && strings.HasPrefix(line, "E "):
			run.Failures[current].Said = append(run.Failures[current].Said, strings.TrimSpace(line[1:]))
		case current >= 0 && pytestLocation().MatchString(line):
			run.Failures[current].At = filepath.ToSlash(pytestLocation().FindStringSubmatch(line)[1])
		}
	}
	if len(run.Failures) == 0 {
		run.Failures = short
	}
	run.Failures = slices.DeleteFunc(run.Failures, func(failure TestFailure) bool {
		file, collecting := strings.CutPrefix(failure.Name, "ERROR collecting ")
		if collecting {
			run.Problems, run.Failed = append(run.Problems, Problem{At: cmp.Or(failure.At, file), Message: strings.Join(failure.Said, "; ")}), max(0, run.Failed-1)
		}
		return collecting
	})
	return run
}

func Unittest(output string) TestRun {
	run, ran, current, framed := TestRun{}, -1, -1, false
	for _, line := range outputLines(output) {
		if match := unittestRan().FindStringSubmatch(line); match != nil {
			ran, current = atoi(match[1]), -1
			continue
		}
		if ran >= 0 && (strings.HasPrefix(line, "OK") || strings.HasPrefix(line, "FAILED")) {
			tally := map[string]int{}
			for _, match := range unittestTally().FindAllStringSubmatch(line, -1) {
				tally[match[1]] = atoi(match[2])
			}
			run.Listed = true
			run.Failed = tally["failures"] + tally["errors"] + tally["unexpected successes"]
			run.Skipped = tally["skipped"] + tally["expected failures"]
			run.Passed = ran - run.Failed - run.Skipped
			continue
		}
		if match := unittestBlock().FindStringSubmatch(line); match != nil {
			name := match[2]
			if !strings.HasSuffix(name, "."+match[1]) {
				name += "." + match[1]
			}
			run.Failures, current, framed = append(run.Failures, TestFailure{Name: name}), len(run.Failures), false
			continue
		}
		if current < 0 {
			continue
		}
		failure := &run.Failures[current]
		match := pythonFrame().FindStringSubmatch(line)
		switch {
		case match != nil:
			failure.At, failure.Said, framed = filepath.ToSlash(match[1])+":"+match[2], nil, true
		case strings.HasPrefix(line, "-----") || strings.HasPrefix(line, "====="):
			framed = false
		case framed && line != "" && !strings.HasPrefix(line, " "):
			failure.Said = append(failure.Said, line)
		}
	}
	importRoot := ""
	for _, failure := range run.Failures {
		parts := strings.Split(failure.Name, ".")
		for count := len(parts) - 1; count > 0 && importRoot == ""; count-- {
			if before, _, found := strings.Cut(failure.At, "/"+strings.Join(parts[:count], "/")+".py:"); found {
				importRoot = before + "/"
			}
		}
	}
	for i := range run.Failures {
		run.Failures[i].At = strings.TrimPrefix(run.Failures[i].At, importRoot)
	}
	return run
}

func PythonProblems(output string) TestRun {
	var report struct {
		GeneralDiagnostics []struct {
			File     string
			Severity string
			Message  string
			Range    struct{ Start struct{ Line, Character int } }
		}
	}
	run := TestRun{}
	if json.Unmarshal([]byte(output), &report) == nil {
		run.Read = true
		for _, found := range report.GeneralDiagnostics {
			if found.Severity == "error" || found.Severity == "warning" {
				at := fmt.Sprintf("%s:%d:%d", filepath.ToSlash(found.File), found.Range.Start.Line+1, found.Range.Start.Character+1)
				run.addProblem(Problem{At: at, Message: strings.SplitN(found.Message, "\n", 2)[0], Warning: found.Severity == "warning"})
			}
		}
		return run
	}
	for _, line := range outputLines(output) {
		run.Read = run.Read || pythonChecked().MatchString(line)
		if match := pythonProblem().FindStringSubmatch(line); match != nil {
			at := filepath.ToSlash(match[1]) + ":" + match[2]
			if match[3] != "" {
				at += ":" + match[3]
			}
			run.addProblem(Problem{At: at, Message: match[5], Warning: match[4] == "warning"})
		}
	}
	return run
}

func atoi(digits string) int {
	value, _ := strconv.Atoi(digits)
	return value
}

func (r TestRun) errors() int {
	return len(r.Problems) - r.warnings()
}

func (r TestRun) warnings() int {
	count := 0
	for _, problem := range r.Problems {
		if problem.Warning {
			count++
		}
	}
	return count
}

func (r TestRun) Explains(exitCode int) bool {
	read := r.Read || r.Listed || r.Failed > 0 || r.PackagesOK+r.PackagesNoTests > 0 || len(r.Problems) > 0
	failing := r.Failed > 0 || len(r.Failures) > 0 || r.errors() > 0
	return read && failing == (exitCode != 0)
}

func (r TestRun) Text() string {
	var head []string
	plural := func(count int, one, many string) string {
		if count == 1 {
			return "1 " + one
		}
		return strconv.Itoa(count) + " " + many
	}
	switch {
	case r.Listed:
		head = append(head, fmt.Sprintf("%d passed", r.Passed), fmt.Sprintf("%d failed", r.Failed))
		if r.Skipped > 0 {
			head = append(head, fmt.Sprintf("%d skipped", r.Skipped))
		}
	case r.Failed > 0:
		head = append(head, fmt.Sprintf("%d failed (passes print only under -v)", r.Failed))
	}
	if !r.Listed && r.PackagesOK > 0 {
		head = append(head, plural(r.PackagesOK, "package ok", "packages ok"))
	}
	if !r.Listed && r.PackagesNoTests > 0 {
		head = append(head, plural(r.PackagesNoTests, "package without tests", "packages without tests"))
	}
	if errors := r.errors(); errors > 0 || len(head) == 0 {
		head = append(head, plural(errors, "error", "errors"))
	}
	if warnings := r.warnings(); warnings > 0 {
		head = append(head, plural(warnings, "warning", "warnings"))
	}
	var lines []string
	for _, failure := range r.Failures {
		line := "FAIL " + failure.Name
		if failure.At != "" {
			line += " " + failure.At
		}
		said := failure.Said[:min(len(failure.Said), konst.TestErrorLines)]
		if len(said) > 0 {
			line += ": " + said[0]
		}
		lines = append(lines, line)
		for _, more := range said[min(1, len(said)):] {
			lines = append(lines, "  "+more)
		}
	}
	for _, problem := range r.Problems {
		lines = append(lines, problem.At+": "+problem.Message)
	}
	shown := lines[:min(len(lines), konst.TestLinesCap)]
	if cut := len(lines) - len(shown); cut > 0 {
		shown = append(shown, fmt.Sprintf("(%d more lines not shown)", cut))
	}
	return strings.Join(append([]string{r.Runner + ": " + strings.Join(head, ", ")}, shown...), "\n") + "\n"
}
