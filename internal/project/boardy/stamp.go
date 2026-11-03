package boardy

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
	"tofu/internal/sys"
)

const (
	FlowsStamps    = "flows"
	TreewideStamps = "treewide"
	DeskStamps     = "desk"
	deskArea       = sys.DeskRoot
)

func StampDir(repo string) string {
	return cmp.Or(os.Getenv("TOFU_STAMP_DIR"), filepath.Join(repo, ".local"))
}

func StampPath(repo, kind, tree string) string {
	return filepath.Join(StampDir(repo), kind, tree)
}

func HeadTree(repo string) (string, []string, error) {
	tree, err := headTreeWithoutDesk(repo)
	if err != nil {
		return "", nil, err
	}
	dirty, err := uncommitted(repo, "*.go", ":(exclude)"+deskArea+"/")
	return tree, dirty, err
}

func headTreeWithoutDesk(repo string) (string, error) {
	tree, err := treeWithout(repo, "HEAD", deskArea)
	if tree == "" && err == nil {
		return gitOut(repo, "", "mktree", "-z")
	}
	return tree, err
}

func treeWithout(repo, tree, drop string) (string, error) {
	first, rest, nested := strings.Cut(drop, "/")
	listing, err := gitOut(repo, "", "ls-tree", "-z", tree)
	if err != nil {
		return "", err
	}
	var kept strings.Builder
	for _, entry := range strings.Split(listing, "\x00") {
		meta, name, _ := strings.Cut(entry, "\t")
		if entry == "" || name == first && !nested {
			continue
		}
		if fields := strings.Fields(meta); name == first && len(fields) == 3 && fields[1] == "tree" {
			sub, err := treeWithout(repo, fields[2], rest)
			if err != nil {
				return "", err
			}
			if sub == "" {
				continue
			}
			entry = fields[0] + " tree " + sub + "\t" + name
		}
		kept.WriteString(entry + "\x00")
	}
	if kept.Len() == 0 {
		return "", nil
	}
	return gitOut(repo, kept.String(), "mktree", "-z")
}

func Unstamped(repo string, kinds ...string) (string, []string, error) {
	tree, err := headTreeWithoutDesk(repo)
	if err != nil {
		return "", nil, err
	}
	var run []string
	for _, kind := range kinds {
		if !stamped(repo, kind, tree) {
			run = append(run, "go run ./bench/"+kind)
		}
	}
	return tree, run, nil
}

func DeskTree(repo string) (string, []string, error) {
	tree, err := gitOut(repo, "", "rev-parse", "HEAD:"+deskArea)
	if err != nil {
		return "", nil, err
	}
	dirty, err := uncommitted(repo, deskArea)
	return tree, dirty, err
}

func stamped(repo, kind, tree string) bool {
	stamp, _ := os.ReadFile(StampPath(repo, kind, tree))
	first, _, _ := strings.Cut(strings.ReplaceAll(string(stamp), "\r\n", "\n"), "\n")
	return first == "tree "+tree
}

type DoneGate struct {
	Area          string
	Tree          string
	DeskTree      string
	Unread        string
	Dirty         []string
	Unstamped     bool
	DeskUnstamped bool
	deskUnread    string
	lost          bool
	owns          []string
	stamps        string
}

func checkDone(repo string, owns []string) DoneGate {
	if len(flowsAreas(owns)) == 0 && len(lostFlowsAreas(repo)) == 0 {
		return DoneGate{}
	}
	return headGate(repo).owning(repo, owns)
}

func lostFlowsAreas(repo string) []string {
	if repo == "" {
		return nil
	}
	mod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil || !slices.Contains(strings.Split(strings.ReplaceAll(string(mod), "\r", ""), "\n"), "module tofu") {
		return nil
	}
	var lost []string
	for _, area := range flowsAreaList() {
		if folder, _ := sys.IsDir(filepath.Join(repo, area)); !folder {
			lost = append(lost, area)
		}
	}
	return lost
}

func headGate(repo string) DoneGate {
	if repo == "" {
		return DoneGate{Unread: "no repository is known for this board, so no stamp can be checked"}
	}
	tree, run, err := Unstamped(repo, FlowsStamps)
	if err != nil {
		return DoneGate{Unread: "HEAD's tree could not be read: " + err.Error()}
	}
	g := DoneGate{Tree: tree, Unstamped: len(run) > 0, stamps: StampDir(repo)}
	if g.DeskTree, err = gitOut(repo, "", "rev-parse", "HEAD:"+deskArea); err != nil {
		g.deskUnread = "HEAD's " + deskArea + " tree could not be read, so no desk stamp can be checked: " + err.Error()
	}
	g.DeskUnstamped = !stamped(repo, DeskStamps, g.DeskTree)
	return g
}

func (g DoneGate) owning(repo string, owns []string) DoneGate {
	if lost := lostFlowsAreas(repo); len(lost) > 0 {
		return DoneGate{lost: true, Unread: strings.Join(lost, ", ") + " is not a folder in " + repo + ", and the done stamp gate names it as a flows area, so it cannot tell which tickets the flows cover until the area list moves with the tree."}
	}
	areas := flowsAreas(owns)
	g.Area, g.owns = strings.Join(areas, ", "), owns
	g.Unstamped = g.Unstamped && slices.ContainsFunc(areas, func(area string) bool { return area != deskArea })
	if g.DeskUnstamped = g.DeskUnstamped && slices.Contains(areas, deskArea); g.DeskUnstamped && g.deskUnread != "" {
		g.Unread, g.DeskUnstamped = g.deskUnread, false
	}
	if g.Area == "" || g.Unread != "" {
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
	if g.lost {
		return g.Unread
	}
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
	if g.DeskUnstamped {
		causes = append(causes, "HEAD's "+deskArea+" tree "+g.DeskTree+" has no passing desk stamp")
	}
	if len(causes) == 0 {
		return ""
	}
	why := strings.Join(causes, ", and ") + ".\nIt owns " + strings.Join(g.owns, ", ") + ", which touches " + g.Area + ", and a move to done needs the owned paths committed and HEAD stamped by ./bench/flows: the Go tree for Go areas, the " + deskArea + " tree for " + deskArea + "."
	if len(g.Dirty) > 0 {
		why += "\nCommit the owned paths first, so the work being closed is the work a stamp covers."
	}
	if g.Unstamped || g.DeskUnstamped {
		why += "\nIn a clean clone of HEAD, run: TOFU_STAMP_DIR=" + g.stamps + " go run ./bench/flows\nIt replays every flow on fresh homes, the desk flows through tofu drive desk on a desk built from HEAD, and stamps the Go tree when every Go flow passes and the " + deskArea + " tree when every desk flow passes."
	}
	return why + "\nOn 2026-10-15 two regressions in turn and sub-agent work reached 0.5.15-rc2 with every ticket accepted on its tests, and the owner's nine-day session waited 55 minutes on two finished agents."
}

func DoneRefusal(repo, id string, owns []string) string {
	ref, err := SplitID(id)
	store, openErr := OpenStore(repo)
	var hooks []rule.Hook
	if err = errors.Join(err, openErr); err == nil {
		hooks, err = store.hooks(ref.Key)
	}
	if err == nil && !slices.ContainsFunc(hooks, func(h rule.Hook) bool { return runsCheck(h, checkDoneStamp) }) {
		return ""
	}
	return checkDone(repo, owns).Refusal()
}

func flowsAreaList() []string {
	return []string{"internal/agent/loop", "internal/tools/registry", "internal/host", "internal/agent/subagents", "internal/store/session", "internal/ui/tui", "internal/cli", "apps/cli", deskArea}
}

func flowsAreas(globs []string) []string {
	var areas []string
	for _, glob := range globs {
		literal, _, _ := strings.Cut(strings.ToLower(strings.ReplaceAll(glob, `\`, "/")), "*")
		for _, area := range flowsAreaList() {
			if (strings.HasPrefix(literal, area+"/") || strings.HasPrefix(area+"/", literal)) && !slices.Contains(areas, area) {
				areas = append(areas, area)
			}
		}
	}
	return areas
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
