package bisect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/history/shadow"
	"tofu/internal/tools/scratch/whatif"
)

const Name = "bisect"

type Bisect struct {
	store  shadow.Store
	root   contract.Root
	limits whatif.Limits
}

func New(dir string, store shadow.Store, limits whatif.Limits) (Bisect, error) {
	root, err := contract.NewRoot(dir)
	return Bisect{store: store, root: root, limits: limits}, err
}

func (Bisect) Name() string { return Name }

func (Bisect) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	return core.Tool{
		Name: Name,
		Description: "finds which recorded turn broke a check by replaying the project as each turn left it, read from the shadow store tofu commits at every turn's start and end, through whatif's overlay, so the working tree never changes. " +
			"since names the first suspect turn by the id trail prints; without it the suspects are every ended turn of the newest session. " +
			"check, path, name and command take whatif's shape. the check runs at the anchor, the project before the first suspect, and after the last suspect, then halves the range between. " +
			"returns the culprit turn, the calls that changed each of its files, its diff, every step with its outcome, and the digest of the failing check. " +
			"a check that fails at the anchor says so and stops, and one that passes after every suspect says nothing broke it",
		Parameters: map[string]any{"type": "object", "required": []string{"check"}, "properties": map[string]any{
			"check":   map[string]any{"type": "string", "enum": []string{string(whatif.Build), string(whatif.Test), string(whatif.Command)}},
			"since":   text,
			"path":    text,
			"name":    text,
			"command": map[string]any{"type": "array", "items": text},
		}},
	}
}

func (b Bisect) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		whatif.Check
		Since string `json:"since"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("bisect: arguments are not the expected shape: %w", err)
	}
	timed, cancel := context.WithTimeout(ctx, konst.ShadowSnapshotTimeoutMillis*time.Millisecond)
	defer cancel()
	recorded, err := b.store.Recorded(timed, konst.ShadowTurnsKept)
	if err != nil {
		return contract.Result{}, fmt.Errorf("bisect: %w", err)
	}
	turns := slices.DeleteFunc(recorded, func(one shadow.Recorded) bool { return !one.Ended() })
	if len(turns) == 0 {
		return contract.Result{Content: "no ended turn has been recorded in the shadow store at " + b.store.Dir() + " yet, so there is nothing to bisect\n", Command: Name}, nil
	}
	first := slices.IndexFunc(turns, func(one shadow.Recorded) bool { return one.Session == turns[len(turns)-1].Session })
	if args.Since != "" {
		first = slices.IndexFunc(turns, func(one shadow.Recorded) bool { return one.ID == args.Since })
	}
	if first < 0 {
		return contract.Result{}, fmt.Errorf("bisect: no ended turn %s is recorded: since takes a turn id as trail prints it", args.Since)
	}
	suspects := turns[first:]
	var paths []string
	for _, one := range suspects {
		for _, change := range one.Changes {
			if !slices.Contains(paths, change.Path) {
				paths = append(paths, change.Path)
			}
		}
	}
	labelOf := func(at int) string {
		if at == 0 {
			return "at the anchor, before turn " + suspects[0].ID
		}
		return fmt.Sprintf("after turn %s, %d of %d", suspects[at-1].ID, at, len(suspects))
	}
	files := map[int]map[string]string{}
	var steps []string
	check := func(at int) (whatif.Answer, error) {
		commit := suspects[0].Start
		if at > 0 {
			commit = suspects[at-1].End
		}
		held, kept, err := b.stateAt(timed, commit, paths)
		if err != nil {
			return whatif.Answer{}, fmt.Errorf("bisect: %s: %w", labelOf(at), err)
		}
		files[at] = held
		answer, err := whatif.Try(ctx, b.root, held, args.Check, b.limits, kept...)
		if err != nil {
			return answer, err
		}
		steps = append(steps, labelOf(at)+": "+answer.Outcome.String())
		return answer, nil
	}
	report := func(head string, answer whatif.Answer) string {
		return head + "\nsteps:\n  " + strings.Join(steps, "\n  ") + "\ncheck: " + answer.Digest + "\ncheck sha256: " + answer.Hash + "\nthe working tree was not changed\n"
	}
	good, bad := 0, len(suspects)
	var failing whatif.Answer
	at := good
	for {
		answer, err := check(at)
		switch {
		case err != nil:
			return contract.Result{}, err
		case answer.Outcome != whatif.Passed && answer.Outcome != whatif.Failed:
			return contract.Failure(report("bisect stopped: "+labelOf(at)+" "+answer.Outcome.String()+", so it cannot tell a good turn from a bad one", answer)), nil
		case at == 0 && answer.Outcome == whatif.Failed:
			return contract.Failure(report("bisect stopped: the check already fails "+labelOf(at)+", so no turn since broke it", answer)), nil
		case at == len(suspects) && answer.Outcome == whatif.Passed:
			return contract.Result{Content: report(fmt.Sprintf("no culprit: the check passes after every one of the %d turns since the anchor", len(suspects)), answer), Command: Name}, nil
		case answer.Outcome == whatif.Failed:
			bad, failing = at, answer
		default:
			good = at
		}
		if at == 0 {
			at = bad
			continue
		}
		if bad-good == 1 {
			break
		}
		at = (good + bad) / 2
	}
	culprit := suspects[bad-1]
	var out strings.Builder
	fmt.Fprintf(&out, "culprit: turn %s, %d of %d since the anchor, session %s, agent %s", culprit.ID, bad, len(suspects), culprit.Session, culprit.Agent)
	var diffs []string
	for _, change := range culprit.Changes {
		fmt.Fprintf(&out, "\n  %s changed by calls %s", change.Path, contract.Listed(change.By))
		diffs = append(diffs, patch.Unified(change.Path, files[good][change.Path], files[bad][change.Path]))
	}
	if len(culprit.Changes) == 0 {
		out.WriteString("\n  the turn changed no file, so the break came from outside any recorded turn while it ran")
	}
	return contract.Result{Content: report(out.String()+"\ndiff:\n"+strings.Join(diffs, "\n"), failing), Command: Name}, nil
}

func (b Bisect) stateAt(ctx context.Context, commit string, paths []string) (map[string]string, []string, error) {
	held := map[string]string{}
	if len(paths) == 0 {
		return held, nil, nil
	}
	listed, err := b.store.Git(ctx, "", append([]string{"ls-tree", "-r", "-z", commit, "--"}, paths...)...)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range strings.FieldsFunc(listed, func(r rune) bool { return r == 0 }) {
		meta, file, found := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !found || len(fields) != 3 {
			return nil, nil, fmt.Errorf("git ls-tree in the shadow store %s printed %q where a mode, type, blob and path belong", b.store.Dir(), entry)
		}
		if held[file], err = b.store.Git(ctx, "", "cat-file", "blob", fields[2]); err != nil {
			return nil, nil, err
		}
	}
	var kept []string
	for _, file := range paths {
		if _, ok := held[file]; ok {
			continue
		}
		if _, err := os.Lstat(filepath.Join(string(b.root), filepath.FromSlash(file))); !errors.Is(err, os.ErrNotExist) {
			kept = append(kept, file)
		}
	}
	return held, kept, nil
}
