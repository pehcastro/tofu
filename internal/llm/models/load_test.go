package models

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"tofu/internal/sys"
)

const probeSlug = "anthropic/probe"

func probeBody(from string) []byte {
	return []byte("use: excluded\nreason: from " + from + "\n")
}

func writeProbe(t *testing.T, root string, from string) {
	t.Helper()
	dir := filepath.Join(root, "models", "anthropic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.yaml"), probeBody(from), 0o644); err != nil {
		t.Fatalf("write %s: %v", dir, err)
	}
}

func probeReason(t *testing.T, layers []Layer) string {
	t.Helper()
	library, err := Load(layers)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	for _, model := range library.Models {
		if model.Slug() == probeSlug {
			return model.Reason
		}
	}
	t.Fatalf("no %s in the library: %v", probeSlug, library.Models)
	return ""
}

func shippedProbe() fstest.MapFS {
	return fstest.MapFS{"models/anthropic/probe.yaml": &fstest.MapFile{Data: probeBody("the shipped library")}}
}

func TestTheModelComesFromTheNamedDirectoryAndNotTheWorkingOne(t *testing.T) {
	named, standing := t.TempDir(), t.TempDir()
	writeProbe(t, filepath.Join(named, sys.StateDirName), "the named directory")
	writeProbe(t, filepath.Join(standing, sys.StateDirName), "the directory the process stood in")
	t.Chdir(standing)

	layers, err := Layers(shippedProbe(), named)
	if err != nil {
		t.Fatalf("layers in %s: %v", named, err)
	}
	got := probeReason(t, layers)
	t.Logf("project layer %q, reason %q", layers[2].Origin, got)
	if got != "from the named directory" {
		t.Fatalf("the library came from %s, the directory the process stood in, and not from %s: %q", standing, named, got)
	}
}

func TestTheProjectModelBeatsTheGlobalWhichBeatsTheShipped(t *testing.T) {
	layers, err := Layers(shippedProbe(), "")
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	if len(layers) != 3 {
		t.Fatalf("expected three layers, got %d", len(layers))
	}
	names := []string{layers[0].Name, layers[1].Name, layers[2].Name}
	t.Logf("layers %v origins %q %q %q", names, layers[0].Origin, layers[1].Origin, layers[2].Origin)
	if names[0] != "library" || names[1] != "global" || names[2] != "project" {
		t.Fatalf("the stack is not library, global, project: %v", names)
	}
	for _, layer := range layers[1:] {
		if filepath.Base(layer.Origin) != sys.StateDirName {
			t.Fatalf("the %s layer is not the state directory itself: %q", layer.Name, layer.Origin)
		}
	}

	if got := probeReason(t, layers); got != "from the shipped library" {
		t.Fatalf("with nothing overriding it the shipped file lost: %q", got)
	}

	writeProbe(t, layers[1].Origin, "the global directory")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(layers[1].Origin, "models")) })
	if got := probeReason(t, layers); got != "from the global directory" {
		t.Fatalf("the global directory did not beat the shipped library: %q", got)
	}

	writeProbe(t, layers[2].Origin, "the project directory")
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(layers[2].Origin, "models")) })
	if got := probeReason(t, layers); got != "from the project directory" {
		t.Fatalf("the project directory did not beat the global one: %q", got)
	}
}
