package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	FlowsStamps    = "flows"
	TreewideStamps = "treewide"
)

func StampDir(repo string) string {
	return cmp.Or(os.Getenv("TOFU_STAMP_DIR"), filepath.Join(repo, ".local"))
}

func StampPath(repo, kind, tree string) string {
	return filepath.Join(StampDir(repo), kind, tree)
}

func HeadTree(repo string) (string, []string, error) {
	tree, err := headTreeWithoutApps(repo)
	if err != nil {
		return "", nil, err
	}
	dirty, err := uncommitted(repo, "*.go", ":(exclude)apps/")
	return tree, dirty, err
}

func headTreeWithoutApps(repo string) (string, error) {
	listing, err := gitOut(repo, "", "ls-tree", "-z", "HEAD")
	if err != nil {
		return "", err
	}
	var kept strings.Builder
	for _, entry := range strings.Split(listing, "\x00") {
		if _, name, _ := strings.Cut(entry, "\t"); entry != "" && name != "apps" {
			kept.WriteString(entry + "\x00")
		}
	}
	return gitOut(repo, kept.String(), "mktree", "-z")
}

func Unstamped(repo string, kinds ...string) (string, []string, error) {
	tree, err := headTreeWithoutApps(repo)
	if err != nil {
		return "", nil, err
	}
	var run []string
	for _, kind := range kinds {
		stamp, _ := os.ReadFile(StampPath(repo, kind, tree))
		if first, _, _ := strings.Cut(strings.ReplaceAll(string(stamp), "\r\n", "\n"), "\n"); first != "tree "+tree {
			run = append(run, "go run ./bench/"+kind)
		}
	}
	return tree, run, nil
}

func DoneRefusal(repo string, owns []string) string {
	area := flowsArea(owns)
	if area == "" {
		return ""
	}
	gated := "it owns " + strings.Join(owns, ", ") + ", which touches " + area + ", and a move to done needs the owned paths committed and HEAD's tree stamped by ./bench/flows"
	if repo == "" {
		return gated + ". No repository is known for this board, so no stamp can be checked."
	}
	tree, run, err := Unstamped(repo, FlowsStamps)
	if err != nil {
		return gated + ". HEAD's tree could not be read: " + err.Error()
	}
	var pathspecs []string
	for _, glob := range owns {
		pathspecs = append(pathspecs, ":(glob,icase)"+glob)
	}
	dirty, err := uncommitted(repo, pathspecs...)
	if err != nil {
		return gated + ". The owned paths could not be checked for uncommitted work: " + err.Error()
	}
	var why []string
	if len(dirty) > 0 {
		why = append(why, "These owned paths have uncommitted changes, so the work being closed is not the work a stamp covers. Commit them first:\n  "+strings.Join(dirty, "\n  "))
	}
	if len(run) > 0 {
		why = append(why, "No passing flows stamp exists for HEAD's tree "+tree+". In a clean clone of HEAD, run: TOFU_STAMP_DIR="+StampDir(repo)+" go run ./bench/flows\nIt replays eight long-session flows on fresh homes in about six minutes and stamps the tree only when every flow passes.")
	}
	if len(why) == 0 {
		return ""
	}
	return gated + ".\n\n" + strings.Join(why, "\n\n") + `

On 2026-10-15 two regressions in turn and sub-agent work reached 0.5.15-rc2 with every ticket accepted on its tests, and the owner's nine-day session waited 55 minutes on two finished agents.`
}

func flowsArea(globs []string) string {
	for _, glob := range globs {
		literal, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(glob, `\`, "/")), "*")
		for _, area := range []string{"internal/turn", "internal/host", "internal/subagent", "interface/tui", "cmd/tofu"} {
			if strings.HasPrefix(literal, area+"/") || strings.HasPrefix(area+"/", literal) {
				return area
			}
		}
	}
	return ""
}

func uncommitted(repo string, pathspecs ...string) ([]string, error) {
	out, err := gitOut(repo, "", append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, pathspecs...)...)
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 3 {
			paths = append(paths, line[3:])
		}
	}
	return paths, err
}

func gitOut(repo, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Stdin = repo, strings.NewReader(stdin)
	out, err := cmd.Output()
	if exited := (*exec.ExitError)(nil); errors.As(err, &exited) {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exited.Stderr)))
	}
	return strings.TrimRight(string(out), "\r\n"), err
}
