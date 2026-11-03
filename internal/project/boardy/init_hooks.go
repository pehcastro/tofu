package boardy

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"tofu/internal/librarian/rules"
	"tofu/internal/sys"
	"tofu/library"
)

type InitMode string

const (
	InitDefault      InitMode = "default"
	InitEmpty        InitMode = "empty"
	InitCustom       InitMode = "custom"
	starterPreset             = "starter"
	initModeFileName          = ".mode"
)

type shippedHook struct {
	id, file string
	source   []byte
}

type hookPlan struct {
	starters, builtin []shippedHook
	keep              func(id string) bool
	mode              InitMode
}

func shippedHooks(preset string) ([]shippedHook, error) {
	dir := path.Join(presetsDir, preset)
	entries, err := fs.ReadDir(library.Files(), dir)
	hooks := make([]shippedHook, 0, len(entries))
	for _, entry := range entries {
		file := path.Join(dir, entry.Name())
		source, readErr := fs.ReadFile(library.Files(), file)
		h, parseErr := rule.ParseHook(source, file)
		if err = errors.Join(err, readErr, parseErr); err != nil {
			return nil, err
		}
		hooks = append(hooks, shippedHook{h.ID, path.Join(libraryRoot, file), source})
	}
	return hooks, err
}

func planHooks(mode InitMode, named []string) (hookPlan, error) {
	starters, err := shippedHooks(starterPreset)
	builtin, builtinErr := shippedHooks(builtinPreset)
	if err = errors.Join(err, builtinErr); err != nil {
		return hookPlan{}, err
	}
	var valid []string
	for _, h := range slices.Concat(starters, builtin) {
		valid = append(valid, h.id)
	}
	plan := hookPlan{starters: starters, builtin: builtin, mode: mode, keep: func(id string) bool { return slices.Contains(named, id) }}
	switch mode {
	case "", InitDefault, InitEmpty:
		if len(named) > 0 {
			return plan, errors.New("--hooks goes with --mode custom")
		}
		plan.keep = func(string) bool { return mode != InitEmpty }
		return plan, nil
	case InitCustom:
	default:
		return plan, fmt.Errorf("mode %q is one of %s, %s, %s", mode, InitDefault, InitEmpty, InitCustom)
	}
	if len(named) == 0 {
		return plan, errors.New("--mode custom keeps the hooks named by --hooks, one or more of: " + strings.Join(valid, ", "))
	}
	for _, id := range named {
		if !slices.Contains(valid, id) {
			return plan, fmt.Errorf("%q is not a library or starter hook: one of %s", id, strings.Join(valid, ", "))
		}
	}
	return plan, nil
}

func (s Store) InitBoard(key, name string, mode InitMode, named []string) (Board, error) {
	plan, err := planHooks(mode, named)
	if err != nil {
		return Board{}, err
	}
	board, err := s.Init(key, name)
	if err != nil {
		return board, err
	}
	return board, s.writeHooks(key, plan)
}

func (s Store) WriteStarterHooks(key string) error {
	plan, err := planHooks(InitDefault, nil)
	if err != nil {
		return err
	}
	return s.writeHooks(key, plan)
}

func (s Store) writeHooks(key string, plan hookPlan) error {
	dir := filepath.Join(s.BoardDir(key), RulesDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, h := range plan.starters {
		path := filepath.Join(dir, h.id+".yaml")
		kept, err := sys.Exists(path)
		switch {
		case err != nil:
			return err
		case kept || !plan.keep(h.id):
			continue
		}
		if err := writeAtomic(path, h.source); err != nil {
			return err
		}
	}
	for _, h := range plan.builtin {
		if plan.keep(h.id) {
			continue
		}
		if err := writeAtomic(filepath.Join(dir, h.id+removedSuffix), []byte(h.file+"\n")); err != nil {
			return err
		}
	}
	if plan.mode != InitEmpty && plan.mode != InitCustom {
		return nil
	}
	return writeAtomic(filepath.Join(dir, initModeFileName), []byte(string(plan.mode)+"\n"))
}

func StarterHookIDs() ([]string, error) {
	hooks, err := shippedHooks(starterPreset)
	ids := make([]string, len(hooks))
	for i, h := range hooks {
		ids[i] = h.id
	}
	return ids, err
}

func Unruled(boards []Board) []Board {
	return slices.DeleteFunc(slices.Clone(boards), func(board Board) bool {
		rules, _ := filepath.Glob(filepath.Join(board.Dir, RulesDirName, "*.yaml"))
		removed, _ := filepath.Glob(filepath.Join(board.Dir, RulesDirName, "*"+removedSuffix))
		chosen, _ := sys.Exists(filepath.Join(board.Dir, RulesDirName, initModeFileName))
		return chosen || len(rules)+len(removed) > 0
	})
}
