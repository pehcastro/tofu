package turn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/subagent"
	"tofu/internal/sys"
)

const EveryToolIsRelativeToTheWorkingDirectory = "you are working inside one directory. every path you name is relative to it and nothing above it exists. "

const SpawnAddendum = "spawn hands one piece of work to a child with its own context and its own list of paths it may write, " +
	"and returns what the child did rather than its transcript: give each separable piece its own child, with paths that do not overlap another child's. " +
	ContractAddendum

const ContractAddendum = "if you were spawned as a child and your own brief names a ticket with an ## Acceptance section, finish with a fenced json block: " +
	`{"claims": [{"line": "<an acceptance line, copied exactly as given>", "command": "<the command you ran for it>", "output": "<what that command printed>", "met": true}], "blocked": ["<a line you could not attempt, and why>"]}` +
	". every acceptance line needs the real command that proved it and the real output it printed, whatever met says: a line with no command and no output behind it is counted as omitted, not as met. " +
	"an empty output is still a pass when the command ran and printed nothing, such as a formatter finding nothing to complain about. " +
	"if your reply already shows another fenced json block, such as the contents of a file you edited, put this one after it: the last fenced json block in your reply is read as the contract, and an earlier one is not. " +
	"a child spawned with no ticket, or whose brief has no ## Acceptance section, sends no such block. " +
	"the shape is at library/general/references/contract.md."

func SubAgentList(defined []subagent.Definition) string {
	enabled := enabledSubAgents(defined)
	if len(enabled) == 0 {
		return ""
	}
	lines := []string{"these sub-agents are yours to name in spawn's agent field, each on its own model with its own instructions: implementation goes to the one whose description fits the piece of work"}
	for _, definition := range enabled {
		lines = append(lines, "- "+definition.Name+": "+definition.Description)
	}
	return strings.Join(lines, "\n")
}

const InstructionsOff = "sends no instruction file at all: " +
	"neither the nearest AGENTS.md or CLAUDE.md at or above the working directory, " +
	"nor your personal AGENTS.md or CLAUDE.md in your home directory"

func Environment(dir string, now time.Time) string {
	return environmentBlock(dir, now, nil)
}

func EnvironmentFromShell(dir string, now time.Time, shell RunShell) string {
	return environmentBlock(dir, now, &shell)
}

func environmentBlock(dir string, now time.Time, shell *RunShell) string {
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
	if shell != nil {
		lines = append(lines, shellLinesFromShell(dir, *shell)...)
	} else {
		lines = append(lines, shellLines(dir)...)
	}
	return strings.Join(append(lines, "</env>"), "\n")
}

func shellLines(dir string) []string {
	shell, err := resolveRunShell(realShellEnv(), realToolchainRunner)
	if err != nil {
		return []string{"shell: " + err.Error()}
	}
	return shellLinesFromShell(dir, shell)
}

type instructionFile struct {
	named string
	text  string
}

func InstructionCap(setting int) int {
	if setting < 1 {
		return konst.ProjectInstructionsBytesDefault
	}
	return setting
}

func ProjectInstructionsInOrder(dir, home string, capBytes int, sources []string) (block string, cut, skipped []string) {
	capBytes = InstructionCap(capBytes)
	var files []instructionFile
	if home != "" {
		for _, global := range [...]string{filepath.Join(sys.StateDir(home), "AGENTS.md"), filepath.Join(home, ".claude", "CLAUDE.md")} {
			if isFile(global) {
				files = append(files, instructionFile{"your personal " + filepath.Base(global), readInstructions(global)})
				break
			}
		}
	}
	chosenIn := map[string]string{}
	for _, name := range sources {
		nearest, found := findUp(dir, name)
		if !found {
			continue
		}
		text := readInstructions(nearest)
		if text == "" {
			continue
		}
		folder := filepath.Dir(nearest)
		if first, taken := chosenIn[folder]; taken {
			skipped = append(skipped, "this project's "+name+", because "+first+" in the same folder comes first")
			continue
		}
		chosenIn[folder] = name
		files = append(files, instructionFile{"this project's " + name, text})
	}

	var written strings.Builder
	for _, file := range files {
		if file.text == "" {
			continue
		}
		entry := "instructions from " + file.named + ", which outrank anything above them that disagrees:\n" + file.text
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
	return written.String(), cut, skipped
}

func readInstructions(path string) string {
	body, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
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
