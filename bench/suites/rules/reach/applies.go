package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

type labelledTask struct {
	workspace string
	agent     string
	task      string
	applies   []string
}

func gpuiMotion() []string {
	return []string{"gpui_eased_transitions", "gpui_frame_only_while_moving", "gpui_judge_optimized", "gpui_motion_interruptible", "gpui_motion_properties", "gpui_notify_smallest_view", "gpui_reduced_motion"}
}

func labelledTasks() []labelledTask {
	return []labelledTask{
		{"gpui", "rust-dev", "make the panel ease in", gpuiMotion()},
		{"gpui", "rust-dev", "make the sidebar slide open with a spring", gpuiMotion()},
		{"gpui", "rust-dev", "fix the text field so pasting works", []string{"gpui_input_through_handler"}},
		{"react", "ts-dev", "fix the button in src/app.tsx", nil},
		{"react", "ts-dev", "fix the dialog in src/app.tsx so Escape closes it", []string{"fe_dialog"}},
	}
}

func workspaceFiles() map[string]map[string]string {
	return map[string]map[string]string{
		"gpui": {
			"Cargo.toml":               "[workspace]\nmembers = [\"crates/ui\"]\n",
			"crates/ui/Cargo.toml":     "[package]\nname = \"ui\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\ngpui = { package = \"gpui-ce\", git = \"https://example.invalid/gpui-ce\" }\n",
			"crates/ui/src/panel.rs":   "pub struct Panel;\n",
			"crates/ui/src/lib.rs":     "pub mod panel;\n",
			"crates/ui/src/sidebar.rs": "pub struct Sidebar;\n",
		},
		"react": {
			"package.json": "{\"name\":\"fe\",\"dependencies\":{\"react\":\"18.0.0\"}}\n",
			"src/app.tsx":  "export const App = () => null\n",
		},
		"svelte": {
			"package.json":            "{\"name\":\"fe\",\"devDependencies\":{\"@sveltejs/kit\":\"2.0.0\",\"svelte\":\"5.0.0\"}}\n",
			"src/routes/+page.svelte": "<script>\n\tlet count = $state(0)\n</script>\n",
		},
		"vue": {
			"package.json": "{\"name\":\"fe\",\"dependencies\":{\"vue\":\"3.5.0\"}}\n",
			"src/App.vue":  "<script setup>\n</script>\n",
		},
		"board": {
			"go.mod":  "module demo\n\ngo 1.24\n",
			"main.go": "package main\n\nfunc main() {}\n",
		},
		"python": {
			"pyproject.toml":   "[project]\nname = \"app\"\nversion = \"0.1.0\"\nrequires-python = \">=3.12\"\ndependencies = [\"httpx\"]\n",
			"src/app/fetch.py": "import httpx\n\n\nasync def fetch(url: str) -> str:\n    async with httpx.AsyncClient() as client:\n        return (await client.get(url)).text\n",
		},
	}
}

func writeWorkspaces(scratch string) error {
	for name, files := range workspaceFiles() {
		for file, body := range files {
			at := filepath.Join(scratch, name, filepath.FromSlash(file))
			if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(at, []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

type appliesRun struct {
	arm     string
	task    labelledTask
	fired   []string
	refused string
}

func measureApplies(classifier string, passes int, tree bool, out io.Writer) error {
	scratch, err := os.MkdirTemp("", "applies-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	binary, _, built, commit, err := buildTofu(scratch, tree)
	if err != nil {
		return err
	}
	decided, err := touchesRules(built)
	if err != nil {
		return err
	}
	if err := writeWorkspaces(scratch); err != nil {
		return err
	}
	arms := []string{"keyword"}
	for pass := 1; pass <= passes; pass++ {
		arms = append(arms, fmt.Sprintf("jev %d", pass))
	}
	var runs []appliesRun
	for at, arm := range arms {
		home := filepath.Join(scratch, fmt.Sprintf("h%d", at))
		judged := arm != "keyword"
		if judged {
			bind := exec.Command(binary, "models", "bind", "classifier", classifier)
			bind.Env = freshHome(home, true)
			if said, err := bind.CombinedOutput(); err != nil {
				return fmt.Errorf("tofu models bind classifier %s: %w: %s", classifier, err, said)
			}
		}
		for _, each := range labelledTasks() {
			runs = append(runs, composeLabelled(binary, filepath.Join(scratch, each.workspace), home, judged, arm, each, decided))
		}
	}
	reportApplies(out, fmt.Sprintf("rule_applies at %s, %s/%s, %s, %s, classifier %s, a fresh home a pass, mock workspaces, labels by one author",
		commit, runtime.GOOS, runtime.GOARCH, runtime.Version(), time.Now().Format(time.DateOnly), classifier), runs)
	return nil
}

func touchesRules(root string) ([]string, error) {
	var decided []string
	touches := regexp.MustCompile(`(?m)^touches:`)
	err := filepath.WalkDir(filepath.Join(root, "library"), func(file string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Base(filepath.Dir(file)) != "rules" || filepath.Ext(file) != ".yaml" {
			return err
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if touches.Match(body) {
			decided = append(decided, strings.Split(filepath.Base(file), "@")[0])
		}
		return nil
	})
	return decided, err
}

func composeLabelled(binary, project, home string, judged bool, arm string, each labelledTask, decided []string) appliesRun {
	command := exec.Command(binary, "run", "--dir", project, "--show-prompt", "--agent", each.agent, each.task)
	command.Dir = project
	command.Env = freshHome(home, judged)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	ran := appliesRun{arm: arm, task: each}
	if err := command.Run(); err != nil {
		said := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		ran.refused = said[len(said)-1] + " (" + err.Error() + ")"
		return ran
	}
	firedLine := regexp.MustCompile(`^part \S+\s+\d+ bytes  from the rule (\S+) in `)
	for line := range strings.Lines(stdout.String()) {
		if match := firedLine.FindStringSubmatch(strings.TrimRight(line, "\r\n")); match != nil && slices.Contains(decided, match[1]) {
			ran.fired = append(ran.fired, match[1])
		}
	}
	return ran
}

func reportApplies(out io.Writer, conditions string, runs []appliesRun) {
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer func() { _ = table.Flush() }()
	_, _ = fmt.Fprintf(table, "%s\n\narm\tlabelled\tright\tmissed\tfired wrong\n", conditions)
	var arms []string
	for _, each := range runs {
		if !slices.Contains(arms, each.arm) {
			arms = append(arms, each.arm)
		}
	}
	for _, arm := range arms {
		labelled, right, wrong := 0, 0, 0
		for _, each := range runs {
			if each.arm != arm {
				continue
			}
			labelled += len(each.task.applies)
			for _, id := range each.fired {
				if slices.Contains(each.task.applies, id) {
					right++
				} else {
					wrong++
				}
			}
		}
		_, _ = fmt.Fprintf(table, "%s\t%d\t%d\t%d\t%d\n", arm, labelled, right, labelled-right, wrong)
	}
	_, _ = fmt.Fprintf(table, "\narm\ttask\tmissed\tfired wrong\n")
	for _, each := range runs {
		missed := slices.DeleteFunc(slices.Clone(each.task.applies), func(id string) bool { return slices.Contains(each.fired, id) })
		wrong := slices.DeleteFunc(slices.Clone(each.fired), func(id string) bool { return slices.Contains(each.task.applies, id) })
		if each.refused != "" {
			missed, wrong = []string{"refused: " + each.refused}, nil
		}
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", each.arm, each.task.task, listOrNone(missed), listOrNone(wrong))
	}
}

func listOrNone(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}
