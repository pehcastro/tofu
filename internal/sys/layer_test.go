package sys

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)

func writeMark(t *testing.T, dir, saying string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make %s: %v", dir, err)
	}
	if err := os.WriteFile(Join(dir, "mark"), []byte(saying), 0o644); err != nil {
		t.Fatalf("write %s: %v", dir, err)
	}
}

func TestTheProjectLayerIsTheNamedDirectoryAndAnEmptyOneIsTheWorkingDirectory(t *testing.T) {
	named, standing := t.TempDir(), t.TempDir()
	writeMark(t, Join(named, StateDirName, "web"), "the named directory")
	writeMark(t, Join(standing, StateDirName, "web"), "the directory the process stood in")
	t.Chdir(standing)

	for _, one := range []struct{ dir, want string }{
		{dir: named, want: "the named directory"},
		{dir: "", want: "the directory the process stood in"},
	} {
		layers, err := Layers(fstest.MapFS{}, "web", one.dir)
		if err != nil {
			t.Fatalf("layers for %q: %v", one.dir, err)
		}
		body, err := fs.ReadFile(layers[2].FS, "mark")
		if err != nil {
			t.Fatalf("the project layer %q reads nothing: %v", layers[2].Origin, err)
		}
		t.Logf("dir %q gave the project layer %q saying %q", one.dir, layers[2].Origin, body)
		if string(body) != one.want {
			t.Fatalf("dir %q resolved to %q, want %q", one.dir, body, one.want)
		}
	}
}

func TestTheLibraryLayerIsTheEmbeddedFilesystemWhereverTheProcessRuns(t *testing.T) {
	t.Chdir(t.TempDir())
	shipped := fstest.MapFS{"fetch.yaml": &fstest.MapFile{Data: []byte("max_bytes: 1\n")}}
	layers, err := Layers(shipped, "web", "")
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	t.Logf("library layer %q from %T", layers[0].Origin, layers[0].FS)
	if layers[0].Origin != Join("library", "web") {
		t.Fatalf("the library layer is not named for the shipped tree: %q", layers[0].Origin)
	}
	if _, err := fs.ReadFile(layers[0].FS, "fetch.yaml"); err != nil {
		t.Fatalf("the library layer does not read its own file away from the repository root: %v", err)
	}
}

func TestTheSubdirectoryIsAppendedToEveryLayerAndAnEmptyOneIsTheRoot(t *testing.T) {
	under, err := Layers(fstest.MapFS{}, "questions", "")
	if err != nil {
		t.Fatalf("under questions: %v", err)
	}
	root, err := Layers(fstest.MapFS{}, "", "")
	if err != nil {
		t.Fatalf("at the root: %v", err)
	}
	for i, name := range []string{"library", "global", "project"} {
		t.Logf("%s: %q against %q", name, root[i].Origin, under[i].Origin)
		if under[i].Name != name || root[i].Name != name {
			t.Fatalf("layer %d is %q and %q, want %q", i, under[i].Name, root[i].Name, name)
		}
		if under[i].Origin != Join(root[i].Origin, "questions") {
			t.Fatalf("layer %d is %q, want %q under %q", i, under[i].Origin, "questions", root[i].Origin)
		}
	}
}
