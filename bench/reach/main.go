package main

import (
	"bytes"
	"cmp"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type run struct {
	number  int
	shape   shape
	refused string
	alone   bool
	reached reached
}

func main() {
	never := flag.Bool("never", false, "list only the items no shape reached")
	flag.Parse()
	if err := measure(*never, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "bench/reach:", err)
		os.Exit(1)
	}
}

func measure(never bool, out io.Writer) error {
	source, err := git(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "reach-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	project, home, binary := filepath.Join(scratch, "p"), filepath.Join(scratch, "h"), filepath.Join(scratch, "tofu.exe")
	if _, err := git(".", "clone", "--quiet", "--shared", source, project); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(project, ".tofu")); err != nil {
		return err
	}
	if err := os.Mkdir(home, 0o755); err != nil {
		return err
	}
	commit, err := git(project, "rev-parse", "--short", "HEAD")
	if err != nil {
		return err
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/tofu")
	build.Dir = project
	if built, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build ./cmd/tofu in the clone: %w: %s", err, built)
	}
	library, err := libraryItems(project)
	if err != nil {
		return err
	}
	var runs []run
	for i, each := range shapes() {
		runs = append(runs, compose(binary, project, home, i+1, each))
	}
	report(out, never, fmt.Sprintf("reach at %s, %s/%s, %s, %s, clean clone, fresh home, no .tofu, no model call",
		commit, runtime.GOOS, runtime.GOARCH, runtime.Version(), time.Now().Format(time.DateOnly)), runs, universe(library, runs))
	return nil
}

func git(dir string, args ...string) (string, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	said, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, said)
	}
	return strings.TrimSpace(string(said)), nil
}

func compose(binary, project, home string, number int, each shape) run {
	args := []string{"run", "--dir", project, "--show-prompt"}
	if each.agent != "" {
		args = append(args, "--agent", each.agent)
	}
	command := exec.Command(binary, append(args, each.task)...)
	command.Dir = project
	command.Env = freshHome(home)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		said := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		return run{number: number, shape: each, refused: said[len(said)-1] + " (" + err.Error() + ")"}
	}
	return run{number: number, shape: each, alone: strings.Contains(stderr.String(), "the lead works alone"), reached: parseReached(stdout.String())}
}

func freshHome(home string) []string {
	var env []string
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(pair, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "TOFU_") || strings.HasPrefix(upper, "XDG_") || strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") {
			continue
		}
		env = append(env, pair)
	}
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		env = append(env, name+"="+home)
	}
	return env
}

func report(out io.Writer, never bool, conditions string, runs []run, items []item) {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer func() { _ = table.Flush() }()
	composed := 0
	_, _ = fmt.Fprintf(table, "%s\n\nshape\tagent\ttask\tstatus\n", conditions)
	for _, each := range runs {
		status := "composed"
		switch {
		case each.refused != "":
			status = "refused: " + each.refused
		case each.alone:
			status = "composed, lead alone"
		}
		if each.refused == "" {
			composed++
		}
		_, _ = fmt.Fprintf(table, "%d\t%s\t%s\t%s\n", each.number, cmp.Or(each.shape.agent, "lead"), each.shape.task, status)
	}
	_, _ = fmt.Fprintf(table, "\nnever reached over the %d of %d shapes that composed\nkind\titems\tlines\tnever\tnever lines\n", composed, len(runs))
	shapesOf := func(one item) []string {
		var numbers []string
		for _, each := range runs {
			if each.reached[one.kind][one.key] {
				numbers = append(numbers, strconv.Itoa(each.number))
			}
		}
		return numbers
	}
	for _, k := range kindOrder() {
		total, lines, missed, missedLines := 0, 0, 0, 0
		for _, one := range items {
			if one.kind != k {
				continue
			}
			total, lines = total+1, lines+one.lines
			if len(shapesOf(one)) == 0 {
				missed, missedLines = missed+1, missedLines+one.lines
			}
		}
		_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\n", k, total, lines, missed, missedLines)
	}
	_, _ = fmt.Fprintf(table, "\nkind\titem\tlines\tshapes\n")
	for _, one := range items {
		reach := strings.Join(shapesOf(one), ",")
		if reach != "" && never {
			continue
		}
		if reach == "" {
			reach = "never"
		}
		lines := strconv.Itoa(one.lines)
		if one.kind == kindTool {
			lines = "-"
		}
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", one.kind, one.key, lines, reach)
	}
}
