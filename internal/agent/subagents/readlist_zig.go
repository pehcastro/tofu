package subagents

import (
	"path"
	"slices"
	"strings"
)

func zigChecks(args []shellWord) error {
	switch first(args) {
	case "version", "fmt":
		return nil
	case "build":
		for _, arg := range args[1:] {
			name, _, _ := strings.Cut(arg.text, "=")
			switch {
			case name == "--":
				return nil
			case slices.Contains([]string{"-p", "--prefix", "--prefix-lib-dir", "--prefix-exe-dir", "--prefix-include-dir", "--build-file", "--build-runner",
				"--zig-lib-dir", "--cache-dir", "--global-cache-dir", "--sysroot", "--search-prefix", "--libc"}, name):
				return ReadListError{Program: "zig build", Why: "with " + arg.text + " builds from or into a folder of its own choosing"}
			}
		}
		return nil
	}
	return ReadListError{Program: "zig " + first(args), Why: "runs here only as zig build, fmt or version: zig build of steps named test, check, lint, typecheck or vet runs for anyone, and of any other step in a build root you hold a path in"}
}

func (c simpleCommand) zigBuilds() []shellWord {
	tool, args := c.unwrapped()
	if tool != "zig" || first(args) != "build" {
		return nil
	}
	var steps []string
	for i := 1; i < len(args) && args[i].text != "--"; i++ {
		switch text := args[i].text; {
		case slices.Contains([]string{"--summary", "--color", "--seed", "--debug-log", "--maxrss", "--error-style", "--multiline-errors"}, text):
			i++
		case !strings.HasPrefix(text, "-"):
			steps = append(steps, text)
		}
	}
	if len(steps) > 0 && !slices.ContainsFunc(steps, func(step string) bool { return !checkName(step) }) {
		return nil
	}
	return []shellWord{{text: "build.zig"}}
}

func (b *Boundary) zigRoots(commands []simpleCommand) error {
	files, err := writesOf(commands, simpleCommand.zigBuilds, nil)
	if err != nil {
		return err
	}
	for _, file := range files {
		root, found := nearest(file, "build.zig")
		if !found {
			return ReadListError{Program: "zig build", Why: "finds no build.zig in " + path.Dir(file) + " or above it"}
		}
		if err := b.holdsEvery("zig build", []string{root}); err != nil {
			return err
		}
	}
	return nil
}
