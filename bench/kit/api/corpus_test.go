package api

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"tofu/bench/corpus"
)

const wantCorpusFileCount = 12

func parseAllJSON(fsys fs.FS) (int, error) {
	names, err := fs.Glob(fsys, "*.json")
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return 0, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return 0, err
		}
	}
	return len(names), nil
}

func TestEveryCorpusFileParsesAndTheCountMatches(t *testing.T) {
	count, err := parseAllJSON(corpus.Files())
	if err != nil {
		t.Fatalf("parsing bench/corpus: %v", err)
	}
	if count != wantCorpusFileCount {
		t.Fatalf("bench/corpus holds %d JSON files, want exactly %d; a file was added or removed without updating this test", count, wantCorpusFileCount)
	}
}

func TestParseAllJSONFailsWhenACorpusFileIsRemoved(t *testing.T) {
	dir := t.TempDir()
	names, err := fs.Glob(corpus.Files(), "*.json")
	if err != nil {
		t.Fatalf("listing bench/corpus: %v", err)
	}
	for _, name := range names {
		raw, err := fs.ReadFile(corpus.Files(), name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatalf("writing copy of %s: %v", name, err)
		}
	}
	before, err := parseAllJSON(os.DirFS(dir))
	if err != nil {
		t.Fatalf("parsing the untouched copy: %v", err)
	}
	if before != wantCorpusFileCount {
		t.Fatalf("copy under t.TempDir() holds %d files, want %d before removing one", before, wantCorpusFileCount)
	}

	if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
		t.Fatalf("removing %s from the temp copy: %v", names[0], err)
	}
	after, err := parseAllJSON(os.DirFS(dir))
	if err != nil {
		t.Fatalf("parsing the copy with one file removed: %v", err)
	}
	if after == wantCorpusFileCount {
		t.Fatalf("removing %s from the temp copy did not change the count, still %d; the count check is not exercising anything", names[0], after)
	}
	if after != wantCorpusFileCount-1 {
		t.Fatalf("count after removing one file = %d, want %d", after, wantCorpusFileCount-1)
	}
}

func TestGateCasesLoadEverySixByteExactFile(t *testing.T) {
	cases, err := GateCases()
	if err != nil {
		t.Fatalf("GateCases: %v", err)
	}
	if len(cases) != len(gateCaseFiles) {
		t.Fatalf("GateCases() returned %d cases, want %d, one per file in gateCaseFiles", len(cases), len(gateCaseFiles))
	}
	for i, c := range cases {
		if c.Name != gateCaseFiles[i] {
			t.Errorf("case %d name = %q, want %q", i, c.Name, gateCaseFiles[i])
		}
		if c.State == nil {
			t.Errorf("case %q parsed to a nil state", c.Name)
		}
	}
}

func TestSizeFixturesLoadEveryLabeledFile(t *testing.T) {
	fixtures, err := SizeFixtures()
	if err != nil {
		t.Fatalf("SizeFixtures: %v", err)
	}
	if len(fixtures) != len(sizeFixtureFiles) {
		t.Fatalf("SizeFixtures() returned %d fixtures, want %d", len(fixtures), len(sizeFixtureFiles))
	}
	for i, f := range fixtures {
		if f.Label != sizeFixtureFiles[i].label {
			t.Errorf("fixture %d label = %q, want %q", i, f.Label, sizeFixtureFiles[i].label)
		}
		if f.State == nil {
			t.Errorf("fixture %q parsed to a nil state", f.Label)
		}
	}
}
