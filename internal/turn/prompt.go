package turn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const PreferTheToolOverTheShell = "prefer the tool that does the thing over a shell command that imitates it: " +
	"project_report answers what is this repository in one call, glob finds files by name, " +
	"search finds text and returns the whole declaration a match sits inside, read reads one file whole, " +
	"edit replaces one exact stretch of text inside one, and write creates one or replaces it whole. " +
	"never run find, ls -R, du or wc over the tree. " +
	"find descends into every directory git ignores, because -not -path filters what it prints and does not stop it walking, " +
	"so one recorded run on this kind of tree spent 68 seconds on a find and then 153 seconds on another, " +
	"while glob answered its question in 15 milliseconds. " +
	"bash is for the project's own commands, its package manager, its build and its tests, " +
	"and for nothing one of those tools already does, and a command it runs is killed at its deadline and its output is lost. " +
	"never write a throwaway script to change a file: that is what edit is. " +
	"tofu_lint_comments, tofu_rules_check and tofu_judge run tofu's own checks with tofu's own parser, " +
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
		for _, global := range [...]string{filepath.Join(sys.StateDir(home), "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
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
	var dropped []string
	for _, path := range paths {
		body, err := os.ReadFile(path)
		text := strings.TrimSpace(string(body))
		if err != nil || text == "" {
			continue
		}
		entry := "instructions from " + path + ", which outrank anything above them that disagrees:\n" + text
		separator := ""
		if block.Len() > 0 {
			separator = "\n\n"
		}
		room := konst.ProjectInstructionsBytes - block.Len() - len(separator)
		if room <= 0 {
			dropped = append(dropped, fmt.Sprintf("%s (%d bytes)", path, len(entry)))
			continue
		}
		if len(entry) > room {
			block.WriteString(separator)
			block.WriteString(entry[:room])
			dropped = append(dropped, fmt.Sprintf("%s (%d of %d bytes)", path, len(entry)-room, len(entry)))
			continue
		}
		block.WriteString(separator)
		block.WriteString(entry)
	}
	if len(dropped) > 0 {
		fmt.Fprintf(&block, "\n\n[capped at %d bytes: dropped %s]", konst.ProjectInstructionsBytes, strings.Join(dropped, ", "))
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
