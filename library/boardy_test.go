package library_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"

	"tofu/internal/boardy"
	"tofu/library"
)

func TestEveryBoardyPresetParsesAndNamesAKnownCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store := boardy.Store{Dir: t.TempDir(), Root: t.TempDir()}
	api := boardy.API{Board: boardy.Managed{Local: boardy.Local{Store: store}}}
	presets, err := fs.ReadDir(library.Files(), "boardy/hooks")
	if err != nil || len(presets) == 0 {
		t.Fatalf("the binary ships no boardy preset: %v", err)
	}
	for _, preset := range presets {
		key := "PRESET"
		if _, err := store.Init(key, preset.Name()); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store.BoardDir(key), ".preset"), []byte(preset.Name()+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		files, _ := fs.ReadDir(library.Files(), path.Join("boardy/hooks", preset.Name()))
		listed, err := api.Rules(boardy.RulesRequest{Board: key})
		if err != nil || len(listed.Invalid) > 0 {
			t.Fatalf("preset %s does not load: %v %q", preset.Name(), err, listed.Invalid)
		}
		shipped := 0
		for _, row := range listed.Hooks {
			if row.Scope == boardy.ScopeLibrary {
				shipped++
			}
		}
		if shipped != len(files) {
			t.Fatalf("preset %s ships %d hook files and the board lists %d of them", preset.Name(), len(files), shipped)
		}
		if err := os.RemoveAll(store.BoardDir(key)); err != nil {
			t.Fatal(err)
		}
	}
}
