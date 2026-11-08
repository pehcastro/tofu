package trail

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

const Name = "trail"

type mode string

const (
	find    mode = "find"
	restore mode = "restore"
	ownerOf mode = "owner_of"
)

var (
	ticketShape = platformfs.LazyRegexp(`^[A-Z][A-Z0-9]*-[0-9]+$`)
	commitShape = platformfs.LazyRegexp(`^[0-9a-f]{7,40}$`)
	timeLayouts = []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"}
)

type Trail struct {
	Store    shadow.Store
	Sessions *session.Store
}

func (Trail) Name() string { return Name }

func (Trail) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "finds what happened around an anchor and how to put it back, read from the shadow store tofu commits at every turn's start and end and from the session record, never from the project's own git history alone. " +
			"mode find takes an anchor: a quote from a message, a message number in the newest session, a time, a ticket id, a commit, or a path, and returns the turns it names with every file each changed, " +
			"who changed it (session, turn, the calls that named it, the agent and who spawned it), the project commit that holds the result if any, the restore step for each change, and every file changed again after that turn. " +
			"mode restore takes ids from find and undoes each turn's change to each file: a file nothing changed since goes back whole, and a text file later turns or the person also changed is merged three ways, keeping their changes, while a change that touches the same lines refuses the whole call naming the file and the lines; dry_run says what it would write and writes nothing. " +
			"mode owner_of takes line as path:number and names the session, turn and message behind that line of the file as it is now, or says it was changed outside any recorded turn. " +
			fmt.Sprintf("only ended turns are listed, at most %d per answer, newest kept.", konst.ChangesTurnsShown),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode":    map[string]any{"type": "string", "enum": []string{string(find), string(restore), string(ownerOf)}},
				"anchor":  map[string]any{"type": "string", "description": "for find: a quote, a message number, a time such as 2026-11-06 05:41, a ticket such as TOFU-12, a commit, or a path"},
				"ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "for restore: change ids as find printed them, turn:path"},
				"dry_run": map[string]any{"type": "boolean"},
				"line":    map[string]any{"type": "string", "description": "for owner_of: path:line, for instance order.go:15"},
			},
		},
	}
}

func (t Trail) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Mode   mode     `json:"mode"`
		Anchor string   `json:"anchor"`
		IDs    []string `json:"ids"`
		DryRun bool     `json:"dry_run"`
		Line   string   `json:"line"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("trail: arguments are not the expected shape: %w", err)
	}
	command := strings.TrimSpace(string(cmp.Or(args.Mode, find)) + " " + cmp.Or(strings.TrimSpace(args.Anchor), args.Line, strings.Join(args.IDs, " ")))
	timed, cancel := context.WithTimeout(ctx, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
	defer cancel()
	turns, err := ended(timed, t.Store)
	if err != nil {
		return contract.Result{}, fmt.Errorf("trail: %w", err)
	}
	if len(turns) == 0 {
		return contract.Result{Content: "no ended turn has been recorded in the shadow store at " + t.Store.Dir() + " yet\n", Command: command}, nil
	}
	var said string
	switch args.Mode {
	case find, "":
		if strings.TrimSpace(args.Anchor) == "" {
			return contract.Result{}, errors.New("trail: find needs an anchor: a quote, a message number, a time, a ticket, a commit or a path")
		}
		said, err = t.find(timed, turns, strings.TrimSpace(args.Anchor))
	case restore:
		said, err = t.restore(timed, turns, args.IDs, args.DryRun)
	case ownerOf:
		said, err = t.ownerOf(timed, turns, args.Line)
	default:
		return contract.Result{}, fmt.Errorf("trail: mode %q is none of find, restore and owner_of", args.Mode)
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("trail %s: %w", cmp.Or(args.Mode, find), err)
	}
	return contract.Result{Content: said, Command: command}, nil
}

func (t Trail) handle(id string) string {
	if named, err := t.Sessions.Identity(id); err == nil {
		return named.Handle()
	}
	return cmp.Or(id, "none")
}

func (t Trail) anchored(ctx context.Context, turns []shadow.Recorded, said []told, anchor string) (string, []int, string, error) {
	var picked []int
	pick := func(keep func(shadow.Recorded) bool) {
		for i, one := range turns {
			if keep(one) {
				picked = append(picked, i)
			}
		}
	}
	saying := func(words string) func(shadow.Recorded) bool {
		return func(one shadow.Recorded) bool {
			return slices.ContainsFunc(said, func(s told) bool {
				return s.turn == one.ID && strings.Contains(strings.ToLower(s.text), strings.ToLower(words))
			})
		}
	}
	if number, err := strconv.Atoi(strings.TrimPrefix(anchor, "#")); err == nil && number > 0 {
		newest := turns[len(turns)-1].Session
		var started []string
		for _, s := range said {
			if s.session == newest && !slices.Contains(started, s.turn) {
				started = append(started, s.turn)
			}
		}
		if number > len(started) {
			return "", nil, "", fmt.Errorf("message %d: the newest session %s holds %d messages from the person", number, t.handle(newest), len(started))
		}
		pick(func(one shadow.Recorded) bool { return one.ID == started[number-1] })
		return "message " + strconv.Itoa(number) + " of " + t.handle(newest), picked, "", nil
	}
	for _, layout := range timeLayouts {
		at, err := time.ParseInLocation(layout, anchor, time.Local)
		if err != nil {
			continue
		}
		if first := slices.IndexFunc(turns, func(one shadow.Recorded) bool { return !one.EndedAt.Before(at) }); first >= 0 {
			picked = []int{first}
		}
		return "the turn running at " + at.Format(time.RFC3339) + " or the first after it", picked, "", nil
	}
	if ticketShape().MatchString(anchor) {
		mentioned := saying(anchor)
		pick(func(one shadow.Recorded) bool { return strings.Contains(one.Agent, anchor) || mentioned(one) })
		return "ticket " + anchor + ", in a sub-agent's brief or in a message", picked, "", nil
	}
	if commitShape().MatchString(anchor) {
		if listed, err := projectGit(ctx, t.Store.Work(), "ls-tree", "-r", "-z", anchor+"^{commit}"); err == nil {
			held := map[string]string{}
			for _, entry := range strings.Split(strings.TrimSuffix(listed, "\x00"), "\x00") {
				if meta, file, found := strings.Cut(entry, "\t"); found {
					held[file] = strings.Fields(meta)[2]
				}
			}
			pick(func(one shadow.Recorded) bool {
				return slices.ContainsFunc(one.Changes, func(c shadow.Change) bool { return held[c.Path] == c.ToBlob })
			})
			return "commit " + anchor + ", the turns whose result it holds", picked, "", nil
		}
	}
	file := path.Clean(strings.TrimPrefix(strings.ReplaceAll(anchor, "\\", "/"), "./"))
	pick(func(one shadow.Recorded) bool {
		return slices.ContainsFunc(one.Changes, func(c shadow.Change) bool { return c.Path == file })
	})
	if len(picked) > 0 {
		return "path " + file, picked, file, nil
	}
	pick(saying(anchor))
	return "quote " + strconv.Quote(anchor), picked, "", nil
}

func (t Trail) find(ctx context.Context, turns []shadow.Recorded, anchor string) (string, error) {
	said, err := spoken(t.Sessions, turns)
	if err != nil {
		return "", err
	}
	kind, picked, only, err := t.anchored(ctx, turns, said, anchor)
	if err != nil {
		return "", err
	}
	if len(picked) == 0 {
		return "trail " + kind + ": no ended turn matches it\n", nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "trail %s: %d of the %d ended turns\n", kind, len(picked), len(turns))
	if dropped := len(picked) - konst.ChangesTurnsShown; dropped > 0 {
		fmt.Fprintf(&out, "the %d oldest are not shown; give a narrower anchor\n", dropped)
		picked = picked[dropped:]
	}
	var restores, conflicts []string
	for _, at := range picked {
		one := turns[at]
		agent := one.Agent
		if one.SpawnedBy != "" {
			agent += ", spawned by " + one.SpawnedBy
		}
		fmt.Fprintf(&out, "anchor: session %s, turn %s, agent %s, ended %s, message %s\n", t.handle(one.Session), one.ID, agent, one.EndedAt.Format(time.RFC3339), task(said, one.ID))
		shown := 0
		for _, c := range one.Changes {
			if only != "" && c.Path != only {
				continue
			}
			shown++
			commit, err := projectGit(ctx, t.Store.Work(), "log", "--reverse", "--format=%h", "--find-object="+c.ToBlob, "--", c.Path)
			commit, _, _ = strings.Cut(strings.TrimSpace(commit), "\n")
			if err != nil || c.ToBlob == shadow.AbsentBlob {
				commit = ""
			}
			fmt.Fprintf(&out, "  change %s: %s -> %s, calls %s, commit %s\n", changeID(one, c), short(c.FromBlob), short(c.ToBlob), contract.Listed(c.By), cmp.Or(commit, "none"))
			step := "writes " + c.Path + " back to " + short(c.FromBlob)
			if c.FromMode == shadow.AbsentMode {
				step = "deletes " + c.Path + ", which the turn created"
			}
			restores = append(restores, changeID(one, c)+": "+step)
			conflict, err := t.conflict(ctx, turns, at, c)
			if err != nil {
				return "", err
			}
			if conflict != "" {
				conflicts = append(conflicts, changeID(one, c)+": "+conflict)
			}
		}
		if shown == 0 {
			out.WriteString("  changed no file\n")
		}
	}
	fmt.Fprintf(&out, "restore, with mode restore and these ids:\n  %s\nchanged since, which restore merges, refusing only where the same lines changed:\n  %s\n", cmp.Or(strings.Join(restores, "\n  "), "none"), cmp.Or(strings.Join(conflicts, "\n  "), "none"))
	return out.String(), nil
}

func (t Trail) conflict(ctx context.Context, turns []shadow.Recorded, at int, c shadow.Change) (string, error) {
	var later []string
	for _, after := range turns[at+1:] {
		if slices.ContainsFunc(after.Changes, func(again shadow.Change) bool { return again.Path == c.Path }) {
			later = append(later, after.ID)
		}
	}
	if len(later) > 0 {
		return c.Path + " changed again by turn " + strings.Join(later, ", "), nil
	}
	current, err := now(ctx, t.Store, c.Path)
	if err != nil || current == c.ToBlob {
		return "", err
	}
	return c.Path + " changed since outside any recorded turn, now " + short(current), nil
}

func (t Trail) restore(ctx context.Context, turns []shadow.Recorded, ids []string, dryRun bool) (string, error) {
	if len(ids) == 0 {
		return "", errors.New("restore needs ids, the change ids find printed as turn:path")
	}
	type picked struct {
		at int
		shadow.Change
	}
	var steps []picked
	for _, id := range ids {
		turnID, file, _ := strings.Cut(id, ":")
		at := slices.IndexFunc(turns, func(one shadow.Recorded) bool { return one.ID == turnID })
		if at < 0 {
			return "", fmt.Errorf("no ended turn %s is recorded, so %s names nothing: run find again", turnID, id)
		}
		which := slices.IndexFunc(turns[at].Changes, func(c shadow.Change) bool { return c.Path == file })
		if which < 0 {
			return "", fmt.Errorf("turn %s changed no file %s, so %s names nothing: run find again", turnID, file, id)
		}
		steps = append(steps, picked{at, turns[at].Changes[which]})
	}
	slices.SortStableFunc(steps, func(a, b picked) int { return b.at - a.at })
	files := map[string]*pending{}
	var order, refused, planned []string
	for _, step := range steps {
		at, known := files[step.Path]
		if !known {
			current, err := now(ctx, t.Store, step.Path)
			if err != nil {
				return "", err
			}
			at = &pending{blob: current}
			files[step.Path], order = at, append(order, step.Path)
		}
		id := changeID(turns[step.at], step.Change)
		if at.blob == step.ToBlob {
			*at = pending{blob: step.FromBlob, mode: step.FromMode, planned: true}
			planned = append(planned, id)
			continue
		}
		clashes, err := t.merge(ctx, at, step.Change)
		if err != nil {
			return "", err
		}
		if len(clashes) > 0 {
			refused = append(refused, step.Path+" from turn "+turns[step.at].ID+": "+strings.Join(clashes, "; "))
			continue
		}
		planned = append(planned, id+", keeping the later changes to it")
	}
	if len(refused) > 0 {
		return "", fmt.Errorf("refused, and nothing was written: %s", strings.Join(refused, "; "))
	}
	if dryRun {
		return "dry run, nothing written: restore would put back " + strings.Join(planned, ", ") + "\n", nil
	}
	for _, name := range order {
		at := files[name]
		if at.merged != nil {
			if _, err := t.Store.Git(ctx, *at.merged, "hash-object", "-w", "--stdin"); err != nil {
				return "", fmt.Errorf("%s: %w", name, err)
			}
		}
		if err := t.Store.Restore(ctx, shadow.Change{Path: name, FromMode: at.mode, FromBlob: at.blob}); err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
	}
	return "restored " + strings.Join(planned, ", ") + "\n", nil
}

func projectGit(ctx context.Context, work string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.quotepath=false"}, args...)...)
	cmd.Dir = work
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool { return strings.HasPrefix(strings.ToUpper(entry), "GIT_") })
	out, err := cmd.Output()
	return string(out), err
}
