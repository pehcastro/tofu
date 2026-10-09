package turn

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/session"
	"tofu/internal/subagent"
)

func claimedTools(registry Registry, store *session.Store, own string) Registry {
	kept := slices.Clone(registry.tools)
	for i, tool := range kept {
		if slices.Contains([]string{"write", "edit", bashToolName}, tool.Name()) {
			kept[i] = claimed{tool: tool, store: store, own: own}
		}
	}
	return NewRegistry(kept...)
}

func othersClaims(store *session.Store, own, tool string) ([]session.Claim, error) {
	if store == nil {
		return nil, nil
	}
	claims, err := store.Claims()
	if err != nil {
		return nil, fmt.Errorf("%s: the side chats' claims do not read, so nothing is written: %w", tool, err)
	}
	return slices.DeleteFunc(claims, func(claim session.Claim) bool { return claim.Session == own }), nil
}

func sideName(claim session.Claim) string {
	return cmp.Or(claim.Name, claim.Session)
}

type claimed struct {
	tool  Tool
	store *session.Store
	own   string
}

func (t claimed) Name() string { return t.tool.Name() }

func (t claimed) Definition() llm.Tool { return t.tool.Definition() }

func (t claimed) check(raw json.RawMessage) (string, error) {
	if inner, checks := t.tool.(bounded); checks {
		if err := inner.refusal(raw); err != nil {
			return "", err
		}
	}
	claims, err := othersClaims(t.store, t.own, t.Name())
	if err != nil || len(claims) == 0 {
		return "", err
	}
	var written []string
	note := ""
	if t.Name() == bashToolName {
		var args bashArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("%s: arguments are not the expected shape: %w", t.Name(), err)
		}
		var unread string
		if written, unread = shellWrites(args.Command); unread != "" {
			note = heldNote(claims, unread)
		}
	} else {
		path, err := pathArg(t.Name(), raw)
		if err != nil {
			return "", err
		}
		written = []string{path}
	}
	tree, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for _, path := range written {
		if local := filepath.FromSlash(path); filepath.IsAbs(local) {
			if inside, err := filepath.Rel(tree, local); err == nil {
				path = inside
			}
		}
		for _, claim := range claims {
			if held, _ := subagent.Matches(path, claim.Owns); held {
				return "", fmt.Errorf("%s refused: %s is held by side chat %s while its turn runs: write it when that turn ends, or leave it to that chat", t.Name(), path, sideName(claim))
			}
		}
	}
	return note, nil
}

func (t claimed) refusal(raw json.RawMessage) error {
	_, err := t.check(raw)
	return err
}

func (t claimed) Run(ctx context.Context, raw json.RawMessage) (Result, error) {
	note, err := t.check(raw)
	if err != nil {
		return Result{}, err
	}
	result, err := t.tool.Run(ctx, raw)
	if note != "" {
		result.Content = strings.TrimSpace(result.Content + "\n" + note)
	}
	return result, err
}

func shellWrites(command string) ([]string, string) {
	var written []string
	unread := ""
	for _, read := range []string{command, strings.ReplaceAll(command, `\`, "/")} {
		targets, err := subagent.ShellWrites(read)
		if err != nil {
			unread = err.Error()
		}
		written = append(written, targets...)
		for _, step := range subagent.ShellSteps(read) {
			written = append(written, step.Changes...)
			if step.Program == "touch" || step.Program == "mkdir" {
				written = append(written, slices.DeleteFunc(slices.Clone(step.Args), func(arg string) bool { return strings.HasPrefix(arg, "-") })...)
			}
		}
	}
	for _, grammar := range []subagent.Grammar{subagent.POSIX, subagent.PowerShell} {
		err := subagent.NewBoundary("", "", nil).Bash(command, grammar)
		var denied subagent.DeniedError
		var listed subagent.ReadListError
		switch {
		case errors.As(err, &listed):
			unread = listed.Program + " " + listed.Why
		case err != nil && !errors.As(err, &denied):
			unread = err.Error()
		}
	}
	return written, unread
}

func heldNote(claims []session.Claim, unread string) string {
	held := make([]string, len(claims))
	for i, claim := range claims {
		held[i] = sideName(claim) + " holds " + strings.Join(claim.Owns, ", ")
	}
	return "note: side chat " + strings.Join(held, "; side chat ") + " while its turn runs, and where this command writes could not be read (" + unread + "), so it ran unchecked: leave those paths to that chat until its turn ends"
}

func sideHeldOwns(tool string, store *session.Store, own string, owns []string) error {
	if len(owns) == 0 {
		return nil
	}
	claims, err := othersClaims(store, own, tool)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		var meets subagent.Roster
		err := meets.Hold(subagent.SubAgent{ID: sideName(claim), Owns: claim.Owns})
		if err == nil {
			err = meets.Hold(subagent.SubAgent{ID: tool, Owns: owns})
		}
		var collision subagent.CollisionError
		if errors.As(err, &collision) {
			return fmt.Errorf("%s refused: owns %q meets %q, which side chat %s holds while its turn runs: leave those paths out, or ask again when that turn ends", tool, collision.Glob, collision.HolderGlob, collision.Holder)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", tool, err)
		}
	}
	return nil
}
