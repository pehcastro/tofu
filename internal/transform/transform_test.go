package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

const honoIndexAfter = `import { createApp } from './app'

const app = createApp()

export default app
`

const honoPackageAfter = `{
  "scripts": {
    "dev": "bun run --hot src/index.ts",
    "test": "bun test"
  },
  "dependencies": {
    "hono": "^4.13.8"
  },
  "devDependencies": {
    "@types/bun": "latest"
  }
}
`

func TestMain(m *testing.M) {
	before := fixtureHash()
	code := m.Run()
	if after := fixtureHash(); after != before {
		fmt.Printf("the package tree changed while the tests ran: %s became %s\n", before, after)
		os.Exit(1)
	}
	os.Exit(code)
}

func fixtureHash() string {
	sum := sha256.New()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum.Write(fmt.Appendf(nil, "%s %d ", path, len(body)))
		sum.Write(body)
		return nil
	})
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "hono", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func seed(t *testing.T, path, content string) string {
	t.Helper()
	root := t.TempDir()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func applyAndCheckPreview(t *testing.T, root, path string, edits []Edit, want string) Preview {
	t.Helper()
	preview, err := Plan(root, path, edits)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if preview.After != want {
		t.Fatalf("planned result was\n%q\nwanted\n%q", preview.After, want)
	}
	if err := Commit(root, preview); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != want {
		t.Fatalf("the file on disk was\n%q\nwanted\n%q", onDisk, want)
	}
	if got := Unified(path, preview.Before, string(onDisk)); got != preview.Diff {
		t.Fatalf("the preview diff was\n%s\nthe diff that resulted was\n%s", preview.Diff, got)
	}
	return preview
}

func TestCreateAppliesAndThePreviewMatches(t *testing.T) {
	root := t.TempDir()
	preview := applyAndCheckPreview(t, root, "store.ts",
		[]Edit{{Kind: Create, Text: honoIndexAfter}}, honoIndexAfter)
	if preview.Before != "" {
		t.Fatalf("a created file had a before of %q", preview.Before)
	}
	if preview.Result() != "created store.ts: 5 lines, 79 bytes" {
		t.Fatalf("create reported %q", preview.Result())
	}
}

func TestReplaceAppliesToIndexAndThePreviewMatches(t *testing.T) {
	before := readFixture(t, "index.ts")
	edits, err := Derive(before, honoIndexAfter)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(edits) != 3 {
		t.Fatalf("derived %d edits from the recorded index.ts rewrite, wanted 3: %+v", len(edits), edits)
	}
	wantAnchors := []string{"import { Hono } from 'hono'", "const app = new Hono()", "app.get('/', (c) => {"}
	for i, edit := range edits {
		if edit.Kind != Replace || edit.Anchor != wantAnchors[i] {
			t.Fatalf("edit %d was %+v, wanted a %s anchored on %q", i+1, edit, Replace, wantAnchors[i])
		}
	}
	if edits[2].Until != "" || edits[2].Text != "" {
		t.Fatalf("the block deletion was %+v, wanted an empty text ending on the blank line", edits[2])
	}
	applyAndCheckPreview(t, seed(t, "index.ts", before), "index.ts", edits, honoIndexAfter)
}

func TestReplaceAppliesToPackageJSONAndThePreviewMatches(t *testing.T) {
	before := readFixture(t, "package.json")
	edits, err := Derive(before, honoPackageAfter)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("derived %d edits from the recorded package.json rewrite, wanted 1: %+v", len(edits), edits)
	}
	applyAndCheckPreview(t, seed(t, "package.json", before), "package.json", edits, honoPackageAfter)
}

func TestAmbiguousAnchorIsRefusedWithItsCandidatesNamed(t *testing.T) {
	before := readFixture(t, "package.json")
	root := seed(t, "package.json", before)
	_, err := Plan(root, "package.json", []Edit{{Kind: Replace, Anchor: "  },", Until: "  },", Text: "  }\n"}})
	var ambiguous AmbiguousAnchor
	if !errors.As(err, &ambiguous) {
		t.Fatalf("a repeated anchor gave %v, wanted an AmbiguousAnchor", err)
	}
	if ambiguous.Field != anchorField {
		t.Fatalf("the refusal named the %s field, wanted the %s", ambiguous.Field, anchorField)
	}
	if len(ambiguous.Matches) != 2 || ambiguous.Matches[0] != 4 || ambiguous.Matches[1] != 7 {
		t.Fatalf("the candidates were %v, wanted lines 4 and 7", ambiguous.Matches)
	}
	onDisk, readErr := os.ReadFile(filepath.Join(root, "package.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(onDisk) != before {
		t.Fatal("a refused edit still changed the file")
	}
}

func TestMissingAnchorIsRefused(t *testing.T) {
	root := seed(t, "package.json", readFixture(t, "package.json"))
	_, err := Plan(root, "package.json", []Edit{{Kind: Replace, Anchor: "nothing like this", Until: "x", Text: ""}})
	var missing AnchorNotFound
	if !errors.As(err, &missing) || missing.Field != anchorField {
		t.Fatalf("an absent anchor gave %v, wanted an AnchorNotFound on the anchor", err)
	}
}

func TestAnEditIsReversedWithinTheTurn(t *testing.T) {
	before := readFixture(t, "index.ts")
	root := seed(t, "index.ts", before)
	edits, err := Derive(before, honoIndexAfter)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Plan(root, "index.ts", edits)
	if err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, preview); err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, preview.Inverse()); err != nil {
		t.Fatalf("reverting: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != before {
		t.Fatalf("after the revert the file was\n%q\nwanted\n%q", onDisk, before)
	}
}

func TestCommitRefusesWhenTheFileMovedUnderThePreview(t *testing.T) {
	before := readFixture(t, "package.json")
	root := seed(t, "package.json", before)
	edits, err := Derive(before, honoPackageAfter)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Plan(root, "package.json", edits)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Commit(root, preview); err == nil {
		t.Fatal("Commit wrote over a file that had changed since the preview")
	}
}

func TestAPathOutsideTheRootIsRefused(t *testing.T) {
	_, err := Plan(t.TempDir(), filepath.Join("..", "escape.txt"), []Edit{{Kind: Create, Text: "x"}})
	if err == nil {
		t.Fatal("Plan accepted a path above its root")
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	if _, err := (Edit{Kind: "rename_symbol"}).On("a\n"); err == nil {
		t.Fatal("an edit kind this catalogue does not hold was applied")
	}
}
