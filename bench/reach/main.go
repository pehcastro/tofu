package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type run struct {
	number     int
	shape      shape
	refused    string
	alone      bool
	kind       string
	frameworks string
	reached    reached
}

func main() {
	never := flag.Bool("never", false, "list only the items no shape reached")
	tree := flag.Bool("tree", false, "build tofu and read the library from the working tree, uncommitted changes included, rather than from a clean clone of HEAD")
	applies := flag.String("applies", "", "compare the touches keyword against the classifier named here, such as typesafe/jev-latest, on the labelled tasks that name no path, in a mock gpui workspace")
	passes := flag.Int("passes", 3, "with -applies, how many times the classifier arm runs")
	flag.Parse()
	var err error
	if *applies != "" {
		err = measureApplies(*applies, *passes, *tree, os.Stdout)
	} else {
		err = measure(*never, *tree, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench/reach:", err)
		os.Exit(1)
	}
}

func buildTofu(scratch string, tree bool) (binary, project, built, commit string, err error) {
	source, err := git(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", "", "", err
	}
	project, binary = filepath.Join(scratch, "p"), filepath.Join(scratch, "tofu.exe")
	if _, err := git(".", "clone", "--quiet", "--shared", source, project); err != nil {
		return "", "", "", "", err
	}
	if err := os.RemoveAll(filepath.Join(project, ".tofu")); err != nil {
		return "", "", "", "", err
	}
	if commit, err = git(project, "rev-parse", "--short", "HEAD"); err != nil {
		return "", "", "", "", err
	}
	built = project
	if tree {
		built, commit = source, commit+"+tree"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/tofu")
	build.Dir = built
	if said, err := build.CombinedOutput(); err != nil {
		return "", "", "", "", fmt.Errorf("go build ./cmd/tofu in %s: %w: %s", built, err, said)
	}
	return binary, project, built, commit, nil
}

func measure(never, tree bool, out io.Writer) error {
	binding, err := boundClassifier()
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "reach-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	binary, project, built, commit, err := buildTofu(scratch, tree)
	if err != nil {
		return err
	}
	home, from := filepath.Join(scratch, "h"), "clean clone"
	if tree {
		from = "working tree with its uncommitted changes, composed in a clean clone"
	}
	kindBy := "the kind by the word list, no classifier bound"
	roles := filepath.Join(home, ".tofu", "roles")
	if err := os.MkdirAll(roles, 0o755); err != nil {
		return err
	}
	if binding != "" {
		if err := os.WriteFile(filepath.Join(roles, "classifier.yaml"), []byte(binding), 0o644); err != nil {
			return err
		}
		kindBy = "the kind by the bound classifier " + strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(binding), "model:"))
	}
	library, err := libraryItems(built)
	if err != nil {
		return err
	}
	if err := writeWorkspaces(scratch); err != nil {
		return err
	}
	board := exec.Command(binary, "boardy", "init", "--key", "DEMO")
	board.Dir, board.Env = filepath.Join(scratch, "board"), freshHome(home, false)
	if said, err := board.CombinedOutput(); err != nil {
		return fmt.Errorf("tofu boardy init in the board workspace: %w: %s", err, said)
	}
	var runs []run
	for i, each := range shapes() {
		dir := project
		if each.workspace != "" {
			dir = filepath.Join(scratch, each.workspace)
		}
		runs = append(runs, compose(binary, dir, home, binding != "", i+1, each))
	}
	report(out, never, fmt.Sprintf("reach at %s, %s/%s, %s, %s, %s, fresh home, no .tofu, no model call, %s",
		commit, runtime.GOOS, runtime.GOARCH, runtime.Version(), time.Now().Format(time.DateOnly), from, kindBy), runs, universe(library, runs))
	return nil
}

func boundClassifier() (string, error) {
	if os.Getenv("TOFU_LIVE") != "1" {
		return "", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	binding, err := os.ReadFile(filepath.Join(home, ".tofu", "roles", "classifier.yaml"))
	if err != nil {
		return "", fmt.Errorf("TOFU_LIVE=1 and no classifier is bound in %s, so jev cannot name a kind: bind one with tofu models bind classifier, or drop TOFU_LIVE for the word list: %w", home, err)
	}
	return string(binding), nil
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

func compose(binary, project, home string, classifierKeys bool, number int, each shape) run {
	args := []string{"run", "--dir", project, "--show-prompt"}
	if each.agent != "" {
		args = append(args, "--agent", each.agent)
	}
	command := exec.Command(binary, append(args, each.task)...)
	command.Dir = project
	command.Env = freshHome(home, classifierKeys)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		said := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		return run{number: number, shape: each, refused: said[len(said)-1] + " (" + err.Error() + ")"}
	}
	kind := "unread"
	if read := regexp.MustCompile(`(?m)^tofu reads this task as (.+?), as (Jev|the word list) reads it`).FindStringSubmatch(stdout.String()); read != nil {
		kind = read[1] + ", " + read[2]
	}
	return run{number: number, shape: each, alone: strings.Contains(stderr.String(), "the lead works alone"), kind: kind, frameworks: detectedFrameworks(binary, project, home, each), reached: parseReached(stdout.String())}
}

func detectedFrameworks(binary, project, home string, each shape) string {
	args := []string{"rules", "index", each.task, "--dir", project, "--json"}
	if each.agent != "" {
		args = append(args, "--agent", each.agent)
	}
	command := exec.Command(binary, args...)
	command.Dir = project
	command.Env = freshHome(home, false)
	said, err := command.Output()
	var index struct {
		Data struct {
			Frameworks []string `json:"frameworks"`
		} `json:"data"`
	}
	if err == nil {
		err = json.Unmarshal(said, &index)
	}
	if err != nil {
		return "unread: " + err.Error()
	}
	return strings.Join(index.Data.Frameworks, ", ")
}

func freshHome(home string, classifierKeys bool) []string {
	var env []string
	for _, pair := range os.Environ() {
		name, _, _ := strings.Cut(pair, "=")
		upper := strings.ToUpper(name)
		if classifierKeys && strings.HasSuffix(upper, "_API_KEY") {
			env = append(env, pair)
			continue
		}
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
	_, _ = fmt.Fprintf(table, "%s\n\nshape\tagent\ttask\tkind\tstatus\n", conditions)
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
		_, _ = fmt.Fprintf(table, "%d\t%s\t%s\t%s\t%s\n", each.number, cmp.Or(each.shape.agent, "lead"), each.shape.task, cmp.Or(each.kind, "-"), status)
	}
	_, _ = fmt.Fprintf(table, "\nshape\tworkspace\tframeworks\n")
	for _, each := range runs {
		_, _ = fmt.Fprintf(table, "%d\t%s\t%s\n", each.number, cmp.Or(each.shape.workspace, "clone"), cmp.Or(each.frameworks, "none"))
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
	dropped := func(one item) bool {
		return slices.ContainsFunc(dropList(), func(each drop) bool { return each.key == one.key })
	}
	for _, k := range kindOrder() {
		total, lines, missed, missedLines := 0, 0, 0, 0
		for _, one := range items {
			if one.kind != k {
				continue
			}
			total, lines = total+1, lines+one.lines
			if k != kindThreshold && !dropped(one) && len(shapesOf(one)) == 0 {
				missed, missedLines = missed+1, missedLines+one.lines
			}
		}
		if k == kindThreshold {
			_, _ = fmt.Fprintf(table, "%s\t%d\t%d\tread by code, not a prompt\t-\n", k, total, lines)
			continue
		}
		_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\n", k, total, lines, missed, missedLines)
	}
	_, _ = fmt.Fprintf(table, "\nkind\titem\tlines\tshapes\n")
	for _, one := range items {
		reach := strings.Join(shapesOf(one), ",")
		switch {
		case dropped(one), one.kind == kindThreshold && never, reach != "" && never:
			continue
		case one.kind == kindThreshold:
			reach = "read by code"
		case reach == "":
			reach = "never"
		}
		lines := strconv.Itoa(one.lines)
		if one.kind == kindTool {
			lines = "-"
		}
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", one.kind, one.key, lines, reach)
	}
	_, _ = fmt.Fprintf(table, "\ndrop list for the owner, nothing deleted\nitem\treason\n")
	for _, each := range dropList() {
		_, _ = fmt.Fprintf(table, "%s\t%s\n", each.key, each.reason)
	}
}
