package turn

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"boji/internal/sys"
)

const PreferTheToolOverTheShell = "prefer the tool that does the thing over a shell command that imitates it: " +
	"project_report answers what is this repository in one call, glob finds files by name, grep searches their text, " +
	"search returns the whole declaration a match sits inside, read reads one file whole, " +
	"edit replaces one exact stretch of text inside one, and write creates one or replaces it whole. " +
	"never run find, ls -R, du or wc over the tree. " +
	"find descends into every directory git ignores, because -not -path filters what it prints and does not stop it walking, " +
	"so one recorded run on this kind of tree spent 68 seconds on a find and then 153 seconds on another, " +
	"while glob answered its question in 15 milliseconds. " +
	"bash is for the project's own commands, its package manager, its build and its tests, " +
	"and for nothing one of those tools already does, and a command it runs is killed at its deadline and its output is lost. " +
	"never write a throwaway script to change a file: that is what edit is. " +
	"boji_lint_comments, boji_rules_check and boji_judge run boji's own checks with boji's own parser, " +
	"so use one of those rather than a shell command or a guess in prose when you want to know whether the work holds up."

func Environment(dir string, now time.Time) string {
	lines := []string{
		"<env>",
		"working directory: " + absolute(dir),
		"platform: " + sys.OS() + "/" + sys.Arch(),
		"today's date: " + now.Format(time.DateOnly),
	}
	if branch := gitBranch(dir); branch != "" {
		lines = append(lines, "git repository: yes", "git branch: "+branch)
	} else {
		lines = append(lines, "git repository: no")
	}
	return strings.Join(append(lines, "</env>"), "\n")
}

func ProjectInstructions(dir, home string) string {
	var paths []string
	if home != "" {
		for _, global := range [...]string{filepath.Join(home, ".boji", "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
			if isFile(global) {
				paths = append(paths, global)
				break
			}
		}
	}
	for _, name := range [...]string{"AGENTS.md", "CLAUDE.md"} {
		if nearest, found := findUp(dir, name); found {
			paths = append(paths, nearest)
		}
	}

	var block strings.Builder
	for _, path := range paths {
		body, err := os.ReadFile(path)
		text := strings.TrimSpace(string(body))
		if err != nil || text == "" {
			continue
		}
		if block.Len() > 0 {
			block.WriteString("\n\n")
		}
		block.WriteString("instructions from " + path + ", which outrank anything above them that disagrees:\n" + text)
	}
	return block.String()
}

func gitBranch(dir string) string {
	head, found := findUp(dir, ".git", "HEAD")
	if !found {
		return ""
	}
	body, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(body)), "ref: refs/heads/")
}

func findUp(dir string, name ...string) (string, bool) {
	at := absolute(dir)
	for {
		if path := filepath.Join(append([]string{at}, name...)...); isFile(path) {
			return path, true
		}
		parent := filepath.Dir(at)
		if parent == at {
			return "", false
		}
		at = parent
	}
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func absolute(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}
