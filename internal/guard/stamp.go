package guard

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"tofu/internal/boardy"
)

const (
	FlowsStamps    = "flows"
	TreewideStamps = "treewide"
)

var flowsAreas = []string{"internal/turn", "internal/host", "internal/subagent", "interface/tui", "cmd/tofu"}

func StampPath(repo, kind, tree string) string {
	return filepath.Join(StampDir(repo), kind, tree)
}

func unstamped(repo string, kinds ...string) (string, []string, error) {
	tree, err := gitOut(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", nil, err
	}
	var run []string
	for _, kind := range kinds {
		if lines := fileLines(StampPath(repo, kind, tree)); len(lines) == 0 || lines[0] != "tree "+tree {
			run = append(run, "go run ./bench/"+kind)
		}
	}
	return tree, run, nil
}

func flowsRefusal(env Env, command string) string {
	if !strings.Contains(command, "boardy") {
		return ""
	}
	var gated, owned []string
	source := ticketsAt(env)
	for _, id := range doneMoves(command) {
		moved, found, err := source.find(strings.ToUpper(id))
		if err != nil {
			return "The flows gate could not read " + id + ": " + err.Error()
		}
		if area := touchedArea(moved.Owns); found && area != "" {
			gated = append(gated, id+" owns "+strings.Join(moved.Owns, ", ")+", which touches "+area)
			for _, glob := range moved.Owns {
				owned = append(owned, ":(glob,icase)"+glob)
			}
		}
	}
	if len(gated) == 0 {
		return ""
	}
	tree, run, err := unstamped(env.Root, FlowsStamps)
	if err != nil {
		return "A move to done is refused because HEAD's tree could not be read, so no stamp can be checked: " + err.Error()
	}
	dirty, err := uncommitted(env.Root, owned...)
	if err != nil {
		return "A move to done is refused because the owned paths could not be checked for uncommitted work: " + err.Error()
	}
	var why []string
	if len(dirty) > 0 {
		why = append(why, "These owned paths have uncommitted changes, so the work being closed is not the work a stamp covers. Commit them first:\n  "+strings.Join(dirty, "\n  "))
	}
	if len(run) > 0 {
		why = append(why, "No passing flows stamp exists for HEAD's tree "+tree+". In a clean clone of HEAD, run: TOFU_STAMP_DIR="+StampDir(env.Root)+" go run ./bench/flows\nIt replays eight long-session flows on fresh homes in about six minutes and stamps the tree only when every flow passes.")
	}
	if len(why) == 0 {
		return ""
	}
	return "A move to done is refused: " + strings.Join(gated, "; ") + ".\n\n" + strings.Join(why, "\n\n") + `

On 2026-10-15 two regressions in turn and sub-agent work reached 0.5.15-rc2 with every ticket accepted on its tests, and the owner's nine-day session waited 55 minutes on two finished agents.`
}

func releaseRefusal(dir string) string {
	tree, run, err := unstamped(dir, FlowsStamps, TreewideStamps)
	if err != nil {
		return "release refused: HEAD's tree could not be read, so no stamp can be checked: " + err.Error()
	}
	staged, err := gitOut(dir, "diff", "--cached", "--name-only", "HEAD")
	if err != nil {
		return "release refused: the staged files could not be read: " + err.Error()
	}
	var why []string
	if outside := slices.DeleteFunc(strings.Fields(staged), releaseFile); len(outside) > 0 {
		why = append(why, "these staged files are not release files, so the commit is not the stamped HEAD plus a version bump:\n  "+strings.Join(outside, "\n  "))
	}
	if len(run) > 0 {
		why = append(why, "HEAD's tree "+tree+" has no passing stamp; in a clean clone of HEAD, with TOFU_STAMP_DIR="+StampDir(dir)+", run:\n  "+strings.Join(run, "\n  "))
	}
	if len(why) == 0 {
		return ""
	}
	return "release refused: " + strings.Join(why, "\nand ") + "\nA release is HEAD, stamped, plus only internal/konst/konst.go, CHANGELOG.md, library/changelog/, docs/ and library/docs/."
}

func releaseFile(path string) bool {
	return path == "internal/konst/konst.go" || path == "CHANGELOG.md" ||
		slices.ContainsFunc([]string{"docs/", "library/docs/", "library/changelog/"}, func(dir string) bool { return strings.HasPrefix(path, dir) })
}

func touchedArea(globs []string) string {
	for _, glob := range globs {
		literal, _, _ := strings.Cut(strings.ReplaceAll(glob, `\`, "/"), "*")
		for _, area := range flowsAreas {
			if foldPrefix(literal, area+"/") || foldPrefix(area+"/", literal) {
				return area
			}
		}
	}
	return ""
}

func doneMoves(command string) []string {
	var ids []string
	for _, words := range shellWords(command) {
		at := slices.Index(words, "boardy")
		if at < 0 || at+1 == len(words) || words[at+1] != "move" {
			continue
		}
		var args []string
		for i := at + 2; i < len(words); i++ {
			name, isFlag := strings.CutPrefix(words[i], "--")
			switch {
			case !isFlag:
				args = append(args, words[i])
			case name != "all" && name != "answers" && name != "thread":
				i++
			}
		}
		if len(args) == 2 && args[1] == string(boardy.Done) {
			ids = append(ids, args[0])
		}
	}
	return ids
}

func shellWords(command string) [][]string {
	var commands [][]string
	var words []string
	var word strings.Builder
	var quote rune
	inWord := false
	flush := func() {
		if inWord {
			words, inWord = append(words, word.String()), false
			word.Reset()
		}
	}
	for _, r := range command {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case strings.ContainsRune(";&|\n()", r):
			flush()
			commands, words = append(commands, words), nil
		case unicode.IsSpace(r):
			flush()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return append(commands, words)
}

func StampDir(repo string) string {
	return cmp.Or(os.Getenv("TOFU_STAMP_DIR"), filepath.Join(repo, ".local"))
}

func HeadTree(repo string) (string, []string, error) {
	tree, err := gitOut(repo, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", nil, err
	}
	dirty, err := uncommitted(repo, "*.go")
	return tree, dirty, err
}

func uncommitted(repo string, pathspecs ...string) ([]string, error) {
	out, err := gitOut(repo, append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, pathspecs...)...)
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 3 {
			paths = append(paths, line[3:])
		}
	}
	return paths, err
}

func gitOut(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	out, err := cmd.Output()
	if exited := (*exec.ExitError)(nil); errors.As(err, &exited) {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exited.Stderr)))
	}
	return strings.TrimRight(string(out), "\r\n"), err
}
