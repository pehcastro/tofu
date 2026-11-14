package race

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/models/llm/core"
	"tofu/internal/platform/scratch"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/scratch/overlay"
	"tofu/internal/tools/scratch/whatif"
)

const (
	Name        = "race"
	keptFolder  = "races"
	keepFirst   = "first"
	keepAll     = "all"
	keptRunFile = "run-%d.json"
)

type Ran struct {
	Model        string
	USD          float64
	Subscription bool
	Requests     int
	TokensIn     int
	TokensOut    int
}

type Entrant func(ctx context.Context, dir, brief string) (Ran, error)

type Limits struct {
	Whatif  whatif.Limits
	Entrant time.Duration
	Kept    time.Duration
	Fewest  int
	Most    int
}

type Race struct {
	root    contract.Root
	limits  Limits
	entrant Entrant
}

func New(dir string, limits Limits, entrant Entrant) (Race, error) {
	root, err := contract.NewRoot(dir)
	return Race{root: root, limits: limits, entrant: entrant}, err
}

func (Race) Name() string { return Name }

func (r Race) Definition() core.Tool {
	text := map[string]any{"type": "string"}
	return core.Tool{
		Name: Name,
		Description: fmt.Sprintf("use when the person asks for %d to %d whole approaches to one job built and compared: race builds them all at once rather than one after another, one brief per approach, the whole of it including its tests. ", r.limits.Fewest, r.limits.Most) +
			"each brief runs on its own sub-agent in its own copy of the project under the scratchpad, so rival approaches that change the same file never meet in the working tree. " +
			"this is the tool for that request, in place of writing each approach into the tree in turn, through checkpoint save and back or by writing both and deleting the loser; whatif edits only files that already exist, so it cannot build a new package. " +
			"pick whatif to try one or two small edits, and edit for one fix. " +
			"the sub-agents run on the person's model with no sub-agents of their own. " +
			"check, path, name and command take whatif's shape and judge each run's edits on the real project through whatif's overlay as soon as the run ends. " +
			"keep says what happens once a run passes, and it follows the person's words. keep all whenever the person wants the approaches compared or wants one of them kept by a rule, the smaller, simpler, faster or cleaner one: every brief runs to its check, nothing is applied, and every run's outcome, check, size and diff come back with a race id; compare them by the person's rule, then call race again with only race set to that id and pick set to the chosen run's number, which applies that run alone. " +
			"keep first, the default, only when any passing approach will do: the first run whose check passes is applied to the working tree and the rest are stopped before their checks, so they cannot be compared; it returns every run's outcome, its cost, its requests and its check digest, and the winner's diff. " +
			"when no run passes nothing is applied and the tree is byte for byte as it was",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"briefs":  map[string]any{"type": "array", "items": text, "minItems": r.limits.Fewest, "maxItems": r.limits.Most},
			"check":   map[string]any{"type": "string", "enum": []string{string(whatif.Build), string(whatif.Test), string(whatif.Command)}},
			"path":    text,
			"name":    text,
			"command": map[string]any{"type": "array", "items": text},
			"keep":    map[string]any{"type": "string", "enum": []string{keepFirst, keepAll}},
			"race":    text,
			"pick":    map[string]any{"type": "integer", "minimum": 1},
		}},
	}
}

type edit struct {
	Changed map[string]string
	Deleted []string
	Was     map[string][32]byte
	diff    string
	added   int
	removed int
}

type run struct {
	edit
	ran     Ran
	err     error
	answer  whatif.Answer
	checked bool
	stopped bool
}

func (one run) passed() bool { return one.checked && one.answer.Outcome == whatif.Passed }

func (r Race) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		whatif.Check
		Briefs []string `json:"briefs"`
		Keep   string   `json:"keep"`
		Race   string   `json:"race"`
		Pick   int      `json:"pick"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("race: arguments are not the expected shape: %w", err)
	}
	if args.Race != "" || args.Pick != 0 {
		return r.pick(args.Race, args.Pick)
	}
	switch {
	case len(args.Briefs) < r.limits.Fewest || len(args.Briefs) > r.limits.Most:
		return contract.Result{}, fmt.Errorf("race: %d briefs, and a race takes %d to %d", len(args.Briefs), r.limits.Fewest, r.limits.Most)
	case slices.ContainsFunc(args.Briefs, func(brief string) bool { return strings.TrimSpace(brief) == "" }):
		return contract.Result{}, errors.New("race: a brief is empty, and every run needs its whole task")
	case !slices.Contains([]whatif.CheckKind{whatif.Build, whatif.Test, whatif.Command}, args.Kind):
		return contract.Result{}, fmt.Errorf("race: check %q is none of build, test or command", args.Kind)
	case args.Kind == whatif.Command && len(args.Command) == 0:
		return contract.Result{}, errors.New("race: check command needs command, the program and its arguments as a list")
	case !slices.Contains([]string{"", keepFirst, keepAll}, args.Keep):
		return contract.Result{}, fmt.Errorf("race: keep %q is neither first nor all", args.Keep)
	}
	keepsAll := args.Keep == keepAll
	command := fmt.Sprintf("%d briefs, check %s", len(args.Briefs), args.Kind)
	racing, stop := context.WithCancel(ctx)
	defer stop()
	trees, copies := make([]string, len(args.Briefs)), make([]map[string][32]byte, len(args.Briefs))
	for i := range args.Briefs {
		made, err := overlay.Make(string(r.root), nil, r.limits.Whatif.StaleOverlay)
		if err != nil {
			return contract.Result{}, fmt.Errorf("race: run %d's copy was not made: %w", i+1, err)
		}
		defer func() { _ = made.Remove() }()
		if trees[i], err = made.Tree(racing); err == nil {
			copies[i], err = hashes(trees[i])
		}
		if err != nil {
			return contract.Result{}, fmt.Errorf("race: run %d's copy was not made: %w", i+1, err)
		}
	}
	runs, done := make([]run, len(args.Briefs)), make(chan int, len(args.Briefs))
	for i, brief := range args.Briefs {
		go func() {
			runs[i] = r.enter(racing, trees[i], brief, copies[i], args.Check)
			done <- i
		}()
	}
	winner := -1
	for range args.Briefs {
		at := <-done
		runs[at].stopped = winner >= 0 && !runs[at].checked
		if !keepsAll && winner < 0 && runs[at].passed() {
			winner = at
			stop()
		}
	}
	var out strings.Builder
	for i, one := range runs {
		fmt.Fprintf(&out, "\nrun %d: %s, %s", i+1, one.outcome(winner, i), one.ran.cost())
		if one.checked {
			fmt.Fprintf(&out, "\n  check: %s\n  check sha256: %s", strings.ReplaceAll(one.answer.Digest, "\n", "\n  "), one.answer.Hash)
		}
		if keepsAll && one.diff != "" {
			fmt.Fprintf(&out, "\n  size: %d files, %d lines added, %d removed\n  diff:\n%s", len(one.Changed)+len(one.Deleted), one.added, one.removed, one.diff)
		}
	}
	if !slices.ContainsFunc(runs, run.passed) {
		return contract.Failure("race: no run passed the check, so nothing was applied and the working tree was not changed" + out.String() + "\n"), nil
	}
	if keepsAll {
		id, err := r.keep(runs)
		if err != nil {
			return contract.Failure("race: runs passed and were not kept for a pick, so nothing was applied: " + err.Error() + out.String() + "\n"), nil
		}
		head := fmt.Sprintf("race %s: every run ran to its check and none was applied; to apply one call race with race %q and pick set to its run number", id, id)
		return contract.Result{Content: head + out.String() + "\n", Command: command}, nil
	}
	won := runs[winner]
	if err := r.apply(won.edit); err != nil {
		return contract.Failure("race: run " + strconv.Itoa(winner+1) + " passed and was not applied: " + err.Error() + out.String() + "\n"), nil
	}
	head := fmt.Sprintf("race: run %d of %d passed first and was applied to the working tree, files changed: %d; the other runs never touched it", winner+1, len(runs), len(won.Changed)+len(won.Deleted))
	return contract.Result{Content: head + out.String() + "\ndiff:\n" + won.diff + "\n", Command: command}, nil
}

func (r Race) enter(ctx context.Context, tree, brief string, copied map[string][32]byte, check whatif.Check) run {
	entering, cancel := context.WithTimeout(ctx, r.limits.Entrant)
	defer cancel()
	ran, err := r.entrant(entering, tree, brief)
	one := run{ran: ran, err: err}
	if err != nil || ctx.Err() != nil {
		return one
	}
	one.edit, one.err = changes(tree, string(r.root), copied)
	if one.err != nil || len(one.Changed)+len(one.Deleted) == 0 {
		return one
	}
	one.answer, one.err = whatif.Try(ctx, r.root, one.Changed, check, r.limits.Whatif, one.Deleted...)
	one.checked = one.err == nil && ctx.Err() == nil
	return one
}

func (one run) outcome(winner, at int) string {
	switch {
	case at == winner:
		return "passed first and was applied"
	case one.stopped:
		return "stopped once run " + strconv.Itoa(winner+1) + " passed, so its check never ran"
	case one.err != nil:
		return "the run failed: " + one.err.Error()
	case one.checked:
		return one.answer.Outcome.String()
	}
	return "the run changed no file, so there was nothing to check"
}

func (ran Ran) cost() string {
	spent := strconv.FormatFloat(ran.USD, 'f', 4, 64) + " USD"
	if ran.Subscription {
		spent = "a subscription, no money"
	}
	said := fmt.Sprintf("cost %s, %d requests, %d tokens in and %d out", spent, ran.Requests, ran.TokensIn, ran.TokensOut)
	if ran.Model != "" {
		said += " on " + ran.Model
	}
	return said
}

func hashes(tree string) (map[string][32]byte, error) {
	held := map[string][32]byte{}
	err := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		body, err := os.ReadFile(path)
		rel, _ := filepath.Rel(tree, path)
		held[filepath.ToSlash(rel)] = sha256.Sum256(body)
		return err
	})
	return held, err
}

func changes(tree, root string, copied map[string][32]byte) (edit, error) {
	now, err := hashes(tree)
	if err != nil {
		return edit{}, err
	}
	paths := slices.Collect(maps.Keys(now))
	for rel := range copied {
		if _, kept := now[rel]; !kept {
			paths = append(paths, rel)
		}
	}
	made := edit{Changed: map[string]string{}, Was: map[string][32]byte{}}
	var diffs []string
	for _, rel := range slices.Sorted(slices.Values(paths)) {
		was, existed := copied[rel]
		is, kept := now[rel]
		if existed && kept && was == is {
			continue
		}
		var prior, body []byte
		if existed {
			made.Was[rel] = was
			prior, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		}
		if kept {
			if body, err = os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel))); err != nil {
				return edit{}, err
			}
			made.Changed[rel] = string(body)
		} else {
			made.Deleted = append(made.Deleted, rel)
		}
		for _, hunk := range patch.Hunks(string(prior), string(body)) {
			made.added, made.removed = made.added+len(hunk.Added), made.removed+len(hunk.Removed)
		}
		diffs = append(diffs, patch.Unified(rel, string(prior), string(body)))
	}
	made.diff = strings.Join(diffs, "\n")
	return made, nil
}

func (r Race) keptRoot() (string, error) {
	scratchRoot, err := scratch.ScratchRootAt(string(r.root))
	return filepath.Join(scratchRoot, keptFolder), err
}

func (r Race) keep(runs []run) (string, error) {
	parent, err := r.keptRoot()
	if err == nil {
		err = os.MkdirAll(parent, 0o755)
	}
	if err != nil {
		return "", err
	}
	entries, _ := os.ReadDir(parent)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > r.limits.Kept {
			_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
		}
	}
	dir, err := os.MkdirTemp(parent, "")
	if err != nil {
		return "", err
	}
	for i, one := range runs {
		if !one.passed() {
			continue
		}
		body, err := json.Marshal(one.edit)
		if err == nil {
			err = os.WriteFile(filepath.Join(dir, fmt.Sprintf(keptRunFile, i+1)), body, 0o644)
		}
		if err != nil {
			return "", errors.Join(err, os.RemoveAll(dir))
		}
	}
	return filepath.Base(dir), nil
}

func (r Race) pick(id string, at int) (contract.Result, error) {
	parent, err := r.keptRoot()
	if err != nil {
		return contract.Result{}, err
	}
	if id == "" || filepath.Base(id) != id || at < 1 {
		return contract.Result{}, fmt.Errorf("race: pick needs race, the id a keep all race returned, and pick, a run number from 1; got race %q and pick %d", id, at)
	}
	dir := filepath.Join(parent, id)
	body, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(keptRunFile, at)))
	if err != nil {
		return contract.Failure(fmt.Sprintf("race: race %s has no kept run %d, so nothing was applied; only a run that passed is kept, a race is gone once one of its runs is picked, and one unpicked for %s is swept\n", id, at, r.limits.Kept)), nil
	}
	var chosen edit
	if err := json.Unmarshal(body, &chosen); err != nil {
		return contract.Result{}, fmt.Errorf("race: race %s run %d is not readable: %w", id, at, err)
	}
	if err := r.apply(chosen); err != nil {
		return contract.Failure("race: run " + strconv.Itoa(at) + " of race " + id + " was not applied: " + err.Error() + "\n"), nil
	}
	_ = os.RemoveAll(dir)
	return contract.Result{Content: fmt.Sprintf("race %s: run %d was applied to the working tree, files changed: %d\n", id, at, len(chosen.Changed)+len(chosen.Deleted)), Command: fmt.Sprintf("pick run %d of %s", at, id)}, nil
}

func (r Race) apply(won edit) error {
	for _, rel := range slices.Concat(slices.Sorted(maps.Keys(won.Changed)), won.Deleted) {
		body, err := os.ReadFile(filepath.Join(string(r.root), filepath.FromSlash(rel)))
		was, existed := won.Was[rel]
		switch {
		case existed && (err != nil || sha256.Sum256(body) != was):
			return fmt.Errorf("%s changed in the working tree since the race ran, so nothing was written", rel)
		case !existed && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s appeared in the working tree since the race ran, so nothing was written", rel)
		}
	}
	for rel, body := range won.Changed {
		path := filepath.Join(string(r.root), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	for _, rel := range won.Deleted {
		if err := os.Remove(filepath.Join(string(r.root), filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}
