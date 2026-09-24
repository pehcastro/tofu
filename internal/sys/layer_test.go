package sys

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestAnEmbeddedLibraryLayerAndAnOnDiskOneDifferOnlyInTheLibrary(t *testing.T) {
	embedded, err := Layers(fstest.MapFS{}, "web")
	if err != nil {
		t.Fatalf("embedded: %v", err)
	}
	onDisk, err := DiskLayers("web")
	if err != nil {
		t.Fatalf("on disk: %v", err)
	}
	t.Logf("embedded library %q, on disk library %q", embedded[0].Origin, onDisk[0].Origin)
	if embedded[0].Origin != Join("library", "web") {
		t.Fatalf("an embedded library layer is not named for the shipped tree: %q", embedded[0].Origin)
	}
	if !strings.HasSuffix(onDisk[0].Origin, Join("library", "web")) || onDisk[0].Origin == embedded[0].Origin {
		t.Fatalf("an on disk library layer is not an absolute path into the shipped tree: %q", onDisk[0].Origin)
	}
	for i := 1; i < 3; i++ {
		if embedded[i] != onDisk[i] {
			t.Fatalf("layer %d differs: %+v against %+v", i, embedded[i], onDisk[i])
		}
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
