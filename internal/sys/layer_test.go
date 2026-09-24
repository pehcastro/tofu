package sys

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestTheLibraryLayerIsTheEmbeddedFilesystemWhereverTheProcessRuns(t *testing.T) {
	t.Chdir(t.TempDir())
	shipped := fstest.MapFS{"fetch.yaml": &fstest.MapFile{Data: []byte("max_bytes: 1\n")}}
	layers, err := Layers(shipped, "web")
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
	under, err := Layers(fstest.MapFS{}, "questions")
	if err != nil {
		t.Fatalf("under questions: %v", err)
	}
	root, err := Layers(fstest.MapFS{}, "")
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
