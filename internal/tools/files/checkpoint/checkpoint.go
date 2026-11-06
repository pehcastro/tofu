package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

const Name = "checkpoint"

const checkpointRefs = "refs/tofu/checkpoints"

const submodule = "160000"

type repo struct{ shadow.Store }

type action string

const (
	save action = "save"
	back action = "back"
	diff action = "diff"
)

type Checkpoint struct{ Store shadow.Store }

func (Checkpoint) Name() string { return Name }

func (Checkpoint) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "saves the project as it is under a label and later puts back only the files this session's own turns changed since, read from the shadow store tofu commits at every turn's start and end, never the project's git and never a stash. " +
			"save label snapshots every file. back label restores each file this session changed after the save to its saved content, and keeps every file somebody else changed since, naming it: " +
			"a file the person edited after this agent last wrote it stays as the person left it. diff label shows what back would do: each changed file with lines added and removed, the symbols the change touches, and whether back restores or keeps it",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{string(save), string(back), string(diff)}},
				"label":  map[string]any{"type": "string", "description": "letters, digits, - and _, for instance before-cache"},
			},
			"required": []string{"action", "label"},
		},
	}
}

func (c Checkpoint) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Action action `json:"action"`
		Label  string `json:"label"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("checkpoint: arguments are not the expected shape: %w", err)
	}
	if args.Label == "" || strings.Trim(args.Label, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") != "" {
		return contract.Result{}, fmt.Errorf("checkpoint: label %q is not letters, digits, - and _", args.Label)
	}
	started, inTurn := shadow.StartedIn(ctx)
	if !inTurn {
		return contract.Result{}, errors.New("checkpoint: answers only inside a turn, because it tells this agent's edits from everybody else's by the turns the shadow store records, and this call runs in none")
	}
	timed, cancel := context.WithTimeout(ctx, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
	defer cancel()
	r := repo{c.Store}
	ref := checkpointRefs + "/" + started.Turn.Session + "/" + args.Label
	var said string
	var err error
	switch args.Action {
	case save:
		said, err = r.save(timed, ref, args.Label, started.Turn)
	case back:
		said, err = r.back(timed, ref, args.Label, started.Turn.ID)
	case diff:
		said, err = r.diff(timed, ref, args.Label, started.Turn.ID)
	default:
		return contract.Result{}, fmt.Errorf("checkpoint: action %q is none of save, back and diff", args.Action)
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("checkpoint %s %s: %w", args.Action, args.Label, err)
	}
	return contract.Result{Content: said, Command: string(args.Action) + " " + args.Label}, nil
}

func (r repo) save(ctx context.Context, ref, label string, turn shadow.Turn) (string, error) {
	tree, err := r.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	turns, err := r.Recorded(ctx, konst.ShadowTurnsKept)
	if err != nil {
		return "", err
	}
	message := []string{"tofu checkpoint " + label, "", "session " + turn.Session, "turn " + turn.ID}
	for _, earlier := range turns {
		if earlier.Session == turn.Session && earlier.ID != turn.ID {
			message = append(message, "before "+earlier.ID)
		}
	}
	commit, err := r.Git(ctx, strings.Join(message, "\n")+"\n", "commit-tree", tree)
	if err != nil {
		return "", err
	}
	_, notSavedYet := r.Git(ctx, "", "rev-parse", "--verify", "--quiet", ref)
	if _, err := r.Git(ctx, "", "update-ref", ref, strings.TrimSpace(commit)); err != nil {
		return "", err
	}
	listed, err := r.Git(ctx, "", "ls-tree", "-r", "-l", "-z", tree)
	if err != nil {
		return "", err
	}
	files, held := 0, 0
	for _, entry := range strings.Split(strings.TrimSuffix(listed, "\x00"), "\x00") {
		if meta, _, found := strings.Cut(entry, "\t"); found {
			size, _ := strconv.Atoi(strings.Fields(meta)[3])
			files, held = files+1, held+size
		}
	}
	replaced := ""
	if notSavedYet == nil {
		replaced = ", replacing the earlier checkpoint of that name"
	}
	return fmt.Sprintf("saved checkpoint %s%s: %d files, %d bytes, in the shadow store at %s\nback %s restores only the files this session changes from here, and diff %s shows them first\n",
		label, replaced, files, held, r.Dir(), label, label), nil
}

type verdict struct {
	shadow.Change
	keptBecause string
}

type plan struct {
	checkpoint, now string
	saveTurn        string
	files           []verdict
}

func (r repo) plan(ctx context.Context, ref, liveTurn string) (plan, error) {
	saved, err := r.Git(ctx, "", "log", "-1", "--format=%H%n%B", ref, "--")
	if err != nil {
		return plan{}, fmt.Errorf("this session saved no checkpoint of that name, so save it first: %w", err)
	}
	var p plan
	var session, body string
	var before []string
	p.checkpoint, body, _ = strings.Cut(saved, "\n")
	for line := range strings.Lines(body) {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch key {
		case "session":
			session = value
		case "turn":
			p.saveTurn = value
		case "before":
			before = append(before, value)
		}
	}
	if p.now, err = r.Snapshot(ctx); err != nil {
		return plan{}, err
	}
	turns, err := r.Recorded(ctx, konst.ShadowTurnsKept)
	if err != nil {
		return plan{}, err
	}
	agentLeft := map[string]string{}
	for _, turn := range turns {
		if turn.Session != session || slices.Contains(before, turn.ID) {
			continue
		}
		start, end := turn.Start, turn.End
		if !turn.Ended() {
			if turn.ID != liveTurn {
				continue
			}
			end = p.now
		}
		if turn.ID == p.saveTurn {
			start = p.checkpoint
		}
		changes, err := r.Changed(ctx, start, end)
		if err != nil {
			return plan{}, err
		}
		for _, file := range changes {
			agentLeft[file.Path] = file.ToBlob
		}
	}
	since, err := r.Changed(ctx, p.checkpoint, p.now)
	if err != nil {
		return plan{}, err
	}
	for _, file := range since {
		left, touched := agentLeft[file.Path]
		judged := verdict{Change: file}
		switch {
		case !touched:
			judged.keptBecause = "changed since the save, and not by this session"
		case left != file.ToBlob:
			judged.keptBecause = "changed by someone other than this session after it last wrote the file"
		case file.FromMode == submodule || file.ToMode == submodule:
			judged.keptBecause = "a submodule, which checkpoint never moves"
		}
		p.files = append(p.files, judged)
	}
	return p, nil
}

func (r repo) back(ctx context.Context, ref, label, liveTurn string) (string, error) {
	p, err := r.plan(ctx, ref, liveTurn)
	if err != nil {
		return "", err
	}
	var restored, kept []string
	for _, file := range p.files {
		if file.keptBecause == "" {
			err := r.Restore(ctx, file.Change)
			if err == nil {
				restored = append(restored, file.Path)
				continue
			}
			file.keptBecause = "could not be written back: " + err.Error()
		}
		kept = append(kept, "  "+file.Path+": "+file.keptBecause+"\n")
	}
	return fmt.Sprintf("back to checkpoint %s, saved in turn %s\nrestored: %s\nkept as they are now: %d\n%s", label, p.saveTurn, contract.Listed(restored), len(kept), strings.Join(kept, "")), nil
}

func (r repo) diff(ctx context.Context, ref, label, liveTurn string) (string, error) {
	p, err := r.plan(ctx, ref, liveTurn)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp(r.Dir(), "checkpoint-diff-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	said := fmt.Sprintf("checkpoint %s, saved in turn %s, against the project now: %d changed\n", label, p.saveTurn, len(p.files))
	for _, file := range p.files {
		patch, err := r.Git(ctx, "", "diff-tree", "-p", "-U0", "--no-renames", p.checkpoint, p.now, "--", ":(literal)"+file.Path)
		if err != nil {
			return "", err
		}
		counts, oldSpans, newSpans := hunks(patch)
		symbols, err := r.touched(ctx, scratch, file.Path, file.FromBlob, oldSpans, nil)
		if err != nil {
			return "", err
		}
		if symbols, err = r.touched(ctx, scratch, file.Path, file.ToBlob, newSpans, symbols); err != nil {
			return "", err
		}
		back := "back restores it"
		if file.keptBecause != "" {
			back = "back keeps it, " + file.keptBecause
		}
		said += fmt.Sprintf("  %s %s, symbols %s: %s\n", file.Path, counts, contract.Listed(symbols), back)
	}
	return said, nil
}

type span struct{ first, last int }

func hunks(patch string) (string, []span, []span) {
	added, removed := 0, 0
	var oldSpans, newSpans []span
	for line := range strings.Lines(patch) {
		if strings.HasPrefix(line, "Binary files ") {
			return "binary", nil, nil
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "@@" {
			continue
		}
		oldSpan, oldCount := spanOf(fields[1])
		newSpan, newCount := spanOf(fields[2])
		removed, added = removed+oldCount, added+newCount
		if oldCount > 0 {
			oldSpans = append(oldSpans, oldSpan)
		}
		if newCount > 0 {
			newSpans = append(newSpans, newSpan)
		}
	}
	return fmt.Sprintf("+%d -%d", added, removed), oldSpans, newSpans
}

func spanOf(field string) (span, int) {
	start, count, hasCount := strings.Cut(field[1:], ",")
	first, _ := strconv.Atoi(start)
	lines := 1
	if hasCount {
		lines, _ = strconv.Atoi(count)
	}
	return span{first, first + lines - 1}, lines
}

func (r repo) touched(ctx context.Context, scratch, path, blob string, spans []span, symbols []string) ([]string, error) {
	if len(spans) == 0 || strings.Trim(blob, "0") == "" {
		return symbols, nil
	}
	body, err := r.Git(ctx, "", "cat-file", "blob", blob)
	if err != nil {
		return nil, err
	}
	written := filepath.Join(scratch, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(written), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(written, []byte(body), 0o600); err != nil {
		return nil, err
	}
	for _, definition := range engine.Index(scratch, []string{path}, func(string) bool { return true }).Definitions {
		if slices.ContainsFunc(spans, func(changed span) bool {
			return changed.first <= definition.LastLine && definition.FirstLine <= changed.last
		}) && !slices.Contains(symbols, definition.Symbol) {
			symbols = append(symbols, definition.Symbol)
		}
	}
	return symbols, nil
}
