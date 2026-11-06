package edit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"tofu/internal/konst"
	rule "tofu/internal/librarian/rules"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/code/symbols"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/files/glob"
)

type Edit struct {
	root     contract.Root
	ledger   *contract.ReadLedger
	checkers *checkers.Typecheckers
	rules    []rule.Rule
}

func (e Edit) Ruling(rules []rule.Rule) Edit {
	e.rules = rules
	return e
}

func NewEdit(dir string) (Edit, error) {
	root, err := contract.NewRoot(dir)
	return Edit{root: root}, err
}

func (e Edit) Reading(ledger *contract.ReadLedger) Edit {
	e.ledger = ledger
	return e
}

func (e Edit) Checking(checkers *checkers.Typecheckers) Edit {
	e.checkers = checkers
	return e
}

func (e Edit) Name() string { return "edit" }

func (e Edit) Definition() core.Tool {
	change := map[string]any{
		"path":        map[string]any{"type": "string"},
		"old_string":  map[string]any{"type": "string"},
		"new_string":  map[string]any{"type": "string"},
		"symbol":      map[string]any{"type": "string"},
		"insert":      map[string]any{"type": "string", "enum": []string{string(Before), string(After)}},
		"replace_all": map[string]any{"type": "boolean"},
	}
	properties := map[string]any{"edits": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": change, "required": []string{"path", "new_string"}}}}
	for name, field := range change {
		properties[name] = field
	}
	return core.Tool{
		Name: "edit",
		Description: "replaces one exact stretch of an existing file. " +
			"old_string is copied from the file character for character and appears exactly once; " +
			"a small file needs no read first, so edit from what search showed you. " +
			"replace_all true replaces every occurrence instead, which is how a name is renamed through a file. " +
			"an empty new_string deletes. " +
			"on a .go, .rs, .ts, .tsx or .py file, symbol names one whole declaration instead, total or Order.total for a method, and new_string becomes all of it, its attributes and decorators included; " +
			"with insert before or after, new_string goes on its own lines just above that declaration and its doc comment, or just below it, one blank line apart, and the declaration stays. " +
			"give old_string or symbol, not both. " +
			"edits lists more changes of the same shape, to this file or others, and the call writes all of them or none. " +
			"returns a unified diff per file, ending on a .ts, .tsx, .go, .rs or .py file with the errors its checker finds: fix them before moving on. " +
			"write creates a file or replaces a whole one",
		Parameters: map[string]any{"type": "object", "properties": properties},
	}
}

type plannedEdit struct {
	resolved string
	preview  patch.Preview
	change   contract.LineChange
	notes    []string
	fires    []rule.Fire
}

func (e Edit) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	changes, err := EditChanges(raw)
	if err != nil {
		return contract.Result{}, err
	}
	var files, skipped []string
	byFile := map[string][]EditChange{}
	for _, change := range changes {
		change.Old, change.New = contract.AsLF(change.Old), contract.AsLF(change.New)
		if len(changes) > 1 && change.Old != "" && change.Old == change.New {
			skipped = append(skipped, "skipped a change to "+change.Path+" whose old_string and new_string are the same text")
			continue
		}
		file := path.Clean(filepath.ToSlash(change.Path))
		if byFile[file] == nil {
			files = append(files, file)
		}
		byFile[file] = append(byFile[file], change)
	}
	if len(files) == 0 {
		return contract.Result{}, errors.New("edit: every change in this call has the same old_string and new_string, so there is nothing to change")
	}
	plans := make([]plannedEdit, len(files))
	for i, file := range files {
		if plans[i], err = e.plan(ctx, byFile[file], len(files) > 1); err != nil {
			return contract.Result{}, fmt.Errorf("%w%s", err, NoneWritten(len(files)))
		}
	}
	for i, plan := range plans {
		if err := patch.Commit(string(e.root), plan.preview); err != nil {
			for _, written := range plans[:i] {
				_ = patch.Commit(string(e.root), patch.Preview{Path: written.preview.Path, Before: written.preview.After, After: written.preview.Before})
			}
			if errors.Is(err, patch.ErrStale) {
				return contract.Result{}, errors.New("edit: " + engine.Note(engine.Stale,
					plan.preview.Path+" is no longer the text edit read, so nothing was written: read it again and edit the text that is there now"))
			}
			return contract.Result{}, fmt.Errorf("edit: %w%s", err, NoneWritten(len(files)))
		}
	}
	shown, targets := make([]string, len(plans)), make([]string, len(plans))
	var fires []rule.Fire
	for i, plan := range plans {
		e.ledger.Rewrote(ctx, plan.preview.Path, []byte(plan.preview.After), plan.change)
		shown[i] = Checked(ctx, e.checkers, e.root, plan.resolved, strings.Join(append([]string{plan.preview.Diff}, plan.notes...), "\n"))
		targets[i] = plan.preview.Path
		fires = append(fires, plan.fires...)
	}
	return contract.Result{Content: strings.Join(append(shown, skipped...), "\n"), Command: strings.Join(targets, " "), Rules: fires}, nil
}

func refusedChange(change EditChange, onFile int) error {
	switch {
	case change.Old == "" && change.Symbol == "" && change.New == "":
		return errors.New("edit: say what is replaced, either old_string for a stretch of text copied from the file or symbol for the whole of one declaration; an empty old_string appends new_string to the end of the file, and write creates a file")
	case change.Old != "" && change.Symbol != "":
		return errors.New("edit: old_string and symbol are the two ways to say what is replaced and a call gives exactly one: symbol replaces a whole declaration and old_string replaces a stretch of text")
	case change.Insert != InPlace && change.Symbol == "":
		return errors.New("edit: insert puts new_string before or after the declaration symbol names, so a call gives insert with symbol only")
	case change.Insert != InPlace && strings.Trim(change.New, "\n") == "":
		return errors.New("edit: insert puts new_string beside a declaration and new_string is empty, so there is nothing to insert")
	case change.All && change.Symbol != "":
		return errors.New("edit: replace_all replaces every occurrence of old_string and symbol names one declaration, so a call gives replace_all with old_string only")
	case change.Old != "" && change.Old == change.New:
		return errors.New("edit: old_string and new_string are the same text, so there is nothing to change")
	case change.Symbol != "" && onFile > 1:
		return fmt.Errorf("edit: symbol replaces a declaration as %s stands on disk, so it is the only change to its file in a call: give the other changes to %s with old_string", change.Path, change.Path)
	}
	return nil
}

func (e Edit) plan(ctx context.Context, changes []EditChange, several bool) (plannedEdit, error) {
	for _, change := range changes {
		if err := refusedChange(change, len(changes)); err != nil {
			return plannedEdit{}, err
		}
	}
	named := changes[0].Path
	resolved, err := e.root.Resolve(named)
	if err != nil {
		return plannedEdit{}, fmt.Errorf("edit: %w", err)
	}
	target := filepath.ToSlash(named)
	var notes []string
	body, err := os.ReadFile(resolved)
	if errors.Is(err, fs.ErrNotExist) && several {
		return plannedEdit{}, fmt.Errorf("edit: %s does not exist, and a call that changes several files repairs no path, since every check read the paths as given: name it exactly, or create it with write", target)
	}
	if errors.Is(err, fs.ErrNotExist) {
		repaired, note, refusal := glob.Repair(e.root, named, false)
		if refusal != nil {
			return plannedEdit{}, fmt.Errorf("edit: %w. edit changes a file that exists: write creates one", refusal)
		}
		target, notes = repaired, append(notes, note)
		if resolved, err = e.root.Resolve(target); err == nil {
			body, err = os.ReadFile(resolved)
		}
	}
	if err != nil {
		return plannedEdit{}, fmt.Errorf("edit: %w", err)
	}

	if bytes.IndexByte(body, 0) >= 0 {
		return plannedEdit{}, errors.New("edit: " + engine.Note(engine.BinarySkipped,
			target+" holds a null byte, so it is not text and replacing a stretch of it would corrupt it"))
	}
	before := string(body)
	text, endings := contract.EndingsOf(before)
	after, quotedOnce := text, true
	for _, change := range changes {
		var note string
		quotedOnce = quotedOnce && change.Symbol == "" && change.Old != "" && strings.Count(after, change.Old) == 1
		if after, note, err = Apply(e.root, target, after, change); err != nil {
			break
		}
		if note != "" {
			notes = append(notes, note)
		}
	}
	var change contract.LineChange
	if err == nil {
		change = contract.ChangeOf(text, after)
	}
	if !e.ledger.Saw(ctx, target, body) {
		if err != nil || changes[0].Symbol != "" || e.ledger.Known(ctx, target) || len(body) > konst.RefusalCarriesWholeUpToBytes {
			return plannedEdit{}, fmt.Errorf("edit: %s has not been read in this session or has changed since, so the edit is refused rather than trusted against a guess: %s",
				target, e.ledger.Refuse(ctx, target, body, change))
		}
		e.ledger.Mark(ctx, target, body)
	}
	if err != nil {
		return plannedEdit{}, err
	}
	if err := e.ledger.Unshown(ctx, target, body, change); err != nil && !quotedOnce {
		return plannedEdit{}, fmt.Errorf("edit: %w", err)
	}
	after = endings.Restore(after)
	if err := symbols.GoStillParses(target, before, after); err != nil {
		return plannedEdit{}, err
	}
	if !filepath.IsLocal(target) {
		return plannedEdit{}, fmt.Errorf("edit: path %q leaves the root it was given", target)
	}
	fires := rule.CheckWrite(e.rules, target, before, after)
	recorded, err := rule.Weigh(fires)
	if err != nil {
		return plannedEdit{}, fmt.Errorf("edit: %w", err)
	}
	notes = append(notes, recorded...)
	preview := patch.Preview{Path: target, Existed: true, Before: before, After: after, Diff: patch.Unified(target, before, after)}
	return plannedEdit{resolved: resolved, preview: preview, change: change, notes: notes, fires: fires}, nil
}
