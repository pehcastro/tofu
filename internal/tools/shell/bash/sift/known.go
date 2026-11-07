package sift

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"tofu/internal/platform/fs"
	"tofu/internal/tools/code/typecheck/checkers"
)

var droppedMiddle = fs.LazyRegexp(`[^\n]*\n\.\.\.\(\d+ bytes dropped here as they arrived\)\.\.\.\n[^\n]*(\n|$)`)

type parser struct {
	runner  string
	read    func(output string) checkers.TestRun
	printed func(lines []string) []string
}

func Fold(result Shell) (string, bool) {
	known, found := parserFor(result.Command)
	if !found {
		return "", false
	}
	output := droppedMiddle().ReplaceAllString(ansi.Strip(result.Stdout+result.Stderr), "\n")
	run := known.read(output)
	if !run.Explains(result.ExitCode) {
		return "", false
	}
	run.Runner = known.runner
	folded := result.Note + run.Text()
	if known.printed != nil {
		folded += printedBlock(known.printed(strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")))
	}
	if result.ExitCode != 0 {
		folded += exitLine(result.ExitCode)
	}
	return folded, true
}

func parserFor(command string) (parser, bool) {
	command = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command), "2>&1"))
	segments := strings.Split(command, "&&")
	for _, before := range segments[:len(segments)-1] {
		if words := strings.Fields(before); len(words) != 2 || words[0] != "cd" {
			return parser{}, false
		}
	}
	last := segments[len(segments)-1]
	if strings.ContainsAny(last, "|;<>&`$") {
		return parser{}, false
	}
	words := strings.Fields(last)
	for len(words) > 0 && strings.Contains(words[0], "=") {
		words = words[1:]
	}
	for len(words) > 1 && (words[0] == "rtk" && words[1] == "proxy" || words[0] == "uv" && words[1] == "run" || words[0] == "poetry" && words[1] == "run") {
		words = words[2:]
	}
	if len(words) == 0 {
		return parser{}, false
	}
	program := strings.TrimSuffix(strings.ToLower(filepath.Base(words[0])), ".exe")
	verb := ""
	for _, word := range words[1:] {
		if !strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "+") {
			verb = word
			break
		}
	}
	switch {
	case program == "go" && verb == "test":
		return parser{"go test", checkers.GoTest, goPrinted}, true
	case program == "go" && slices.Contains([]string{"vet", "build"}, verb):
		return parser{"go " + verb, checkers.GoTest, nil}, true
	case program == "cargo" && verb == "test":
		return parser{"cargo test", checkers.CargoTest, nil}, true
	case program == "cargo" && slices.Contains([]string{"check", "build", "clippy"}, verb):
		return parser{"cargo " + verb, checkers.RustProblems, nil}, true
	case program == "rustc":
		return parser{"rustc", checkers.RustProblems, nil}, true
	case program == "pytest" || program == "py.test":
		return parser{"pytest", checkers.Pytest, pytestPrinted}, true
	case program == "pyright" || program == "mypy":
		return parser{program, checkers.PythonProblems, nil}, true
	case slices.Contains([]string{"python", "python3", "py"}, program):
		at := slices.Index(words, "-m")
		switch {
		case at < 0 || at+1 == len(words):
		case words[at+1] == "pytest":
			return parser{"pytest", checkers.Pytest, pytestPrinted}, true
		case words[at+1] == "unittest":
			return parser{"unittest", checkers.Unittest, unittestPrinted}, true
		case words[at+1] == "mypy":
			return parser{"mypy", checkers.PythonProblems, nil}, true
		}
	}
	return parser{}, false
}
