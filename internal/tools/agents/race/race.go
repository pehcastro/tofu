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
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/scratch/overlay"
	"tofu/internal/tools/scratch/whatif"
)

const Name = "race"

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
		Description: fmt.Sprintf("races %d to %d sub-agents on rival briefs for one job, each in its own copy of the project under the scratchpad, so no two touch one file. ", r.limits.Fewest, r.limits.Most) +
			"briefs are the whole tasks, one sub-agent each, all running at once on the person's model with no sub-agents of their own. " +
			"check, path, name and command take whatif's shape and judge each run's edits on the real project through whatif's overlay as soon as the run ends. " +
			"the first run whose check passes is applied to the working tree and the rest are stopped and never touch it. " +
			"returns every run's outcome, its cost, its requests and its check digest, and the winner's diff. when no run passes nothing is applied and the tree is byte for byte as it was",
		Parameters: map[string]any{"type": "object", "required": []string{"briefs", "check"}, "properties": map[string]any{
			"briefs":  map[string]any{"type": "array", "items": text, "minItems": r.limits.Fewest, "maxItems": r.limits.Most},
			"check":   map[string]any{"type": "string", "enum": []string{string(whatif.Build), string(whatif.Test), string(whatif.Command)}},
			"path":    text,
			"name":    text,
			"command": map[string]any{"type": "array", "items": text},
		}},
	}
}

type run struct {
	ran     Ran
	err     error
	copied  map[string][32]byte
	changed map[string]string
	deleted []string
	diff    string
	answer  whatif.Answer
	checked bool
	stopped bool
}

func (r Race) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		whatif.Check
		Briefs []string `json:"briefs"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("race: arguments are not the expected shape: %w", err)
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
	}
	racing, stop := context.WithCancel(ctx)
	defer stop()
	trees, copies := make([]string, len(args.Briefs)), make([]map[string][32]byte, len(args.Briefs))
	for i := range args.Briefs {
		made, err := overlay.Make(string(r.root), nil, r.limits.Whatif.StaleOverlay)
		if err != nil {
			return contract.Result{}, fmt.Errorf("race: run %d's copy was not made: %w", i+1, err)
		}
		defer func() { _ = made.Remove() }()
		if trees[i], err = made.Tree(); err == nil {
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
		if winner < 0 && runs[at].checked && runs[at].answer.Outcome == whatif.Passed {
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
	}
	if winner < 0 {
		return contract.Failure("race: no run passed the check, so nothing was applied and the working tree was not changed" + out.String() + "\n"), nil
	}
	won := runs[winner]
	if err := r.apply(won); err != nil {
		return contract.Failure("race: run " + strconv.Itoa(winner+1) + " passed and was not applied: " + err.Error() + out.String() + "\n"), nil
	}
	head := fmt.Sprintf("race: run %d of %d passed first and was applied to the working tree, files changed: %d; the other runs never touched it", winner+1, len(runs), len(won.changed)+len(won.deleted))
	return contract.Result{Content: head + out.String() + "\ndiff:\n" + won.diff + "\n", Command: Name + " " + string(args.Kind)}, nil
}

func (r Race) enter(ctx context.Context, tree, brief string, copied map[string][32]byte, check whatif.Check) run {
	entering, cancel := context.WithTimeout(ctx, r.limits.Entrant)
	defer cancel()
	ran, err := r.entrant(entering, tree, brief)
	one := run{ran: ran, err: err, copied: copied}
	if err != nil || ctx.Err() != nil {
		return one
	}
	one.changed, one.deleted, one.diff, one.err = changes(tree, string(r.root), copied)
	if one.err != nil || len(one.changed)+len(one.deleted) == 0 {
		return one
	}
	one.answer, one.err = whatif.Try(ctx, r.root, one.changed, check, r.limits.Whatif, one.deleted...)
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

func changes(tree, root string, copied map[string][32]byte) (map[string]string, []string, string, error) {
	now, err := hashes(tree)
	if err != nil {
		return nil, nil, "", err
	}
	paths := slices.Collect(maps.Keys(now))
	for rel := range copied {
		if _, kept := now[rel]; !kept {
			paths = append(paths, rel)
		}
	}
	changed := map[string]string{}
	var deleted, diffs []string
	for _, rel := range slices.Sorted(slices.Values(paths)) {
		was, existed := copied[rel]
		is, kept := now[rel]
		if existed && kept && was == is {
			continue
		}
		var prior, body []byte
		if existed {
			prior, _ = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		}
		if kept {
			if body, err = os.ReadFile(filepath.Join(tree, filepath.FromSlash(rel))); err != nil {
				return nil, nil, "", err
			}
			changed[rel] = string(body)
		} else {
			deleted = append(deleted, rel)
		}
		diffs = append(diffs, patch.Unified(rel, string(prior), string(body)))
	}
	return changed, deleted, strings.Join(diffs, "\n"), nil
}

func (r Race) apply(won run) error {
	for _, rel := range slices.Concat(slices.Sorted(maps.Keys(won.changed)), won.deleted) {
		body, err := os.ReadFile(filepath.Join(string(r.root), filepath.FromSlash(rel)))
		was, existed := won.copied[rel]
		switch {
		case existed && (err != nil || sha256.Sum256(body) != was):
			return fmt.Errorf("%s changed in the working tree while the race ran, so nothing was written", rel)
		case !existed && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%s appeared in the working tree while the race ran, so nothing was written", rel)
		}
	}
	for rel, body := range won.changed {
		path := filepath.Join(string(r.root), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	for _, rel := range won.deleted {
		if err := os.Remove(filepath.Join(string(r.root), filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	return nil
}
