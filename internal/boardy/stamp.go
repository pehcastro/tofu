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

type DoneGate struct {
	Area      string
	Tree      string
	Unread    string
	Dirty     []string
	Unstamped bool
	owns      []string
	stamps    string
}

func checkDone(repo string, owns []string) DoneGate {
	if flowsArea(owns) == "" {
		return DoneGate{}
	}
	return headGate(repo).owning(repo, owns)
}

func headGate(repo string) DoneGate {
	if repo == "" {
		return DoneGate{Unread: "no repository is known for this board, so no stamp can be checked"}
	}
	tree, run, err := Unstamped(repo, FlowsStamps)
	if err != nil {
		return DoneGate{Unread: "HEAD's tree could not be read: " + err.Error()}
	}
	return DoneGate{Tree: tree, Unstamped: len(run) > 0, stamps: StampDir(repo)}
}

func (g DoneGate) owning(repo string, owns []string) DoneGate {
	if g.Area, g.owns = flowsArea(owns), owns; g.Area == "" || g.Unread != "" {
		return g
	}
	var pathspecs []string
	for _, glob := range owns {
		pathspecs = append(pathspecs, ":(glob,icase)"+glob)
	}
	var err error
	if g.Dirty, err = uncommitted(repo, pathspecs...); err != nil {
		g.Unread = "the owned paths could not be checked for uncommitted work: " + err.Error()
	}
	return g
}

func (g DoneGate) Refusal() string {
	if g.Area == "" {
		return ""
	}
	var causes []string
	if g.Unread != "" {
		causes = append(causes, g.Unread)
	}
	if len(g.Dirty) > 0 {
		causes = append(causes, "uncommitted owned paths "+strings.Join(g.Dirty, ", "))
	}
	if g.Unstamped {
		causes = append(causes, "HEAD's tree "+g.Tree+" has no passing flows stamp")
	}
	if len(causes) == 0 {
		return ""
	}
	why := strings.Join(causes, ", and ") + ".\nIt owns " + strings.Join(g.owns, ", ") + ", which touches " + g.Area + ", and a move to done needs the owned paths committed and HEAD's tree stamped by ./bench/flows."
	if len(g.Dirty) > 0 {
		why += "\nCommit the owned paths first, so the work being closed is the work a stamp covers."
	}
	if g.Unstamped {
		why += "\nIn a clean clone of HEAD, run: TOFU_STAMP_DIR=" + g.stamps + " go run ./bench/flows\nIt replays eight long-session flows on fresh homes in about six minutes and stamps the tree only when every flow passes."
	}
	return why + "\nOn 2026-10-15 two regressions in turn and sub-agent work reached 0.5.15-rc2 with every ticket accepted on its tests, and the owner's nine-day session waited 55 minutes on two finished agents."
}

func DoneRefusal(repo string, owns []string) string {
	return checkDone(repo, owns).Refusal()
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
