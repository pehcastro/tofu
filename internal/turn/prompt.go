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

const EveryToolIsRelativeToTheWorkingDirectory = "you are working inside one directory. every path you name is relative to it and nothing above it exists. "

const SpawnAddendum = "spawn hands one piece of work to a child with its own context and its own list of paths it may write, " +
	"and returns what the child did rather than its transcript: use it when a piece of the task is separable and its paths do not overlap another child's."

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

const InstructionsOff = "sends no instruction file at all: " +
	"neither the nearest AGENTS.md or CLAUDE.md at or above the working directory, " +
	"nor your personal AGENTS.md or CLAUDE.md in your home directory"

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
	lines = append(lines, shellLines(dir)...)
	return strings.Join(append(lines, "</env>"), "\n")
}

func shellLines(dir string) []string {
	choice, err := resolveShell(realShellEnv())
	if err != nil {
		return []string{"shell: " + err.Error()}
	}
	lines := []string{"shell: " + choice.Label}
	if choice.Note != "" {
		lines = append(lines, "shell notes: "+choice.Note)
	}
	toolchain := probeToolchain(dir, realToolchainRunner, konst.ToolchainProbeTimeoutMillis*time.Millisecond)
	if summary := formatToolchain(toolchain); summary != "" {
		lines = append(lines, "toolchain: "+summary)
	}
	return lines
}

type instructionFile struct {
	path  string
	named string
}

func InstructionCap(setting int) int {
	if setting < 1 {
		return konst.ProjectInstructionsBytesDefault
	}
	return setting
}

func ProjectInstructions(dir, home string, capBytes int) (block string, cut []string) {
	capBytes = InstructionCap(capBytes)
	var files []instructionFile
	if home != "" {
		for _, global := range [...]string{filepath.Join(sys.StateDir(home), "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
			if isFile(global) {
				files = append(files, instructionFile{global, "your personal " + filepath.Base(global)})
				break
			}
		}
	}
	for _, name := range [...]string{"AGENTS.md", "CLAUDE.md"} {
		if nearest, found := findUp(dir, name); found {
			files = append(files, instructionFile{nearest, "this project's " + name})
		}
	}

	var written strings.Builder
	for _, file := range files {
		body, err := os.ReadFile(file.path)
		text := strings.TrimSpace(string(body))
		if err != nil || text == "" {
			continue
		}
		entry := "instructions from " + file.named + ", which outrank anything above them that disagrees:\n" + text
		separator := ""
		if written.Len() > 0 {
			separator = "\n\n"
		}
		room := max(capBytes-written.Len()-len(separator), 0)
		if len(entry) > room {
			cut = append(cut, fmt.Sprintf("%s (%d of %d bytes)", file.named, len(entry)-room, len(entry)))
		}
		if room == 0 {
			continue
		}
		written.WriteString(separator)
		written.WriteString(entry[:min(len(entry), room)])
	}
	if len(cut) > 0 {
		fmt.Fprintf(&written, "\n\n[capped at %d bytes: dropped %s]", capBytes, strings.Join(cut, ", "))
	}
	return written.String(), cut
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
