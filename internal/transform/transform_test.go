package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	lastHunkFirst := []string{"app.get('/', (c) => {", "const app = new Hono()", "import { Hono } from 'hono'"}
	for i, edit := range edits {
		if edit.Kind != Replace || edit.Anchor != lastHunkFirst[i] {
			t.Fatalf("edit %d was %+v, wanted a %s anchored on %q", i+1, edit, Replace, lastHunkFirst[i])
		}
	}
	if edits[0].Until != "" || edits[0].Text != "" {
		t.Fatalf("the block deletion was %+v, wanted an empty text ending on the blank line", edits[0])
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

func TestAnInsertionAboveTheFirstLineAppliesAndThePreviewMatches(t *testing.T) {
	before := readFixture(t, "index.ts")
	after := "import 'dotenv/config'\n" + before
	edits, err := Derive(before, after)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if len(edits) != 1 || edits[0].Kind != Prepend || edits[0].Text != "import 'dotenv/config'\n" {
		t.Fatalf("derived %+v, wanted one %s carrying the inserted line alone", edits, Prepend)
	}
	applyAndCheckPreview(t, seed(t, "index.ts", before), "index.ts", edits, after)
}

func TestADisambiguatedUntilSelectsTheIntendedOccurrence(t *testing.T) {
	before := readFixture(t, "package.json")
	edit := Edit{Kind: Replace, Anchor: "{", Until: "  },", Text: "{\n  \"private\": true,\n"}

	_, err := Plan(seed(t, "package.json", before), "package.json", []Edit{edit})
	var ambiguous AmbiguousAnchor
	if !errors.As(err, &ambiguous) || ambiguous.Field != untilField {
		t.Fatalf("a repeated until gave %v, wanted an AmbiguousAnchor on the %s", err, untilField)
	}
	if len(ambiguous.Matches) != 2 || ambiguous.Matches[0] != 4 || ambiguous.Matches[1] != 7 {
		t.Fatalf("the candidates were %v, wanted lines 4 and 7", ambiguous.Matches)
	}

	wanted := map[int]string{
		1: "{\n  \"private\": true,\n  \"dependencies\": {\n    \"hono\": \"^4.13.8\"\n  },\n" +
			"  \"devDependencies\": {\n    \"@types/bun\": \"latest\"\n  }\n}\n",
		2: "{\n  \"private\": true,\n  \"devDependencies\": {\n    \"@types/bun\": \"latest\"\n  }\n}\n",
	}
	for occurrence, want := range wanted {
		chosen := edit
		chosen.UntilOccurrence = occurrence
		applyAndCheckPreview(t, seed(t, "package.json", before), "package.json", []Edit{chosen}, want)
	}

	beyond := edit
	beyond.UntilOccurrence = 3
	_, err = Plan(seed(t, "package.json", before), "package.json", []Edit{beyond})
	var out OccurrenceOutOfRange
	if !errors.As(err, &out) || out.Found != 2 {
		t.Fatalf("a third occurrence of a line that appears twice gave %v", err)
	}
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
	revert := Preview{Path: preview.Path, Before: preview.After, After: preview.Before}
	if err := Commit(root, revert); err != nil {
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
	err = Commit(root, preview)
	if err == nil {
		t.Fatal("Commit wrote over a file that had changed since the preview")
	}
	t.Logf("refusal: %v", err)
	if !errors.Is(err, ErrStale) {
		t.Fatalf("a caller cannot tell staleness from a write failure: %v", err)
	}
}

func TestAFailureThatIsNotStalenessDoesNotCarryTheStaleError(t *testing.T) {
	root := seed(t, filepath.Join("pkg", "index.ts"), "a\n")
	err := Commit(root, Preview{Path: "pkg", Existed: true, Before: "", After: "b\n"})
	if err == nil {
		t.Fatal("Commit wrote a file where a directory stands")
	}
	t.Logf("refusal: %v", err)
	if errors.Is(err, ErrStale) {
		t.Fatalf("a failed write was reported as a stale file: %v", err)
	}
}

func TestAPreviewSaysWhetherTheFileWasCreatedOrReplaced(t *testing.T) {
	root := seed(t, "held.txt", "")
	made, err := Plan(root, "fresh.txt", []Edit{{Kind: Create, Text: "one\ntwo\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if made.Existed {
		t.Fatal("a file that is not on disk was previewed as existing")
	}
	if want := "created fresh.txt: 2 lines, 8 bytes"; made.Result() != want {
		t.Fatalf("Result reads %q, want %q", made.Result(), want)
	}

	empty, err := Plan(root, "held.txt", []Edit{{Kind: Create, Text: "one\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Existed {
		t.Fatal("a file on disk holding nothing was previewed as absent")
	}
	if strings.Contains(empty.Result(), "created") {
		t.Fatalf("replacing an empty file read as a creation: %q", empty.Result())
	}

	same, err := Plan(root, "held.txt", []Edit{{Kind: Create, Text: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(same.Result(), "nothing changed") {
		t.Fatalf("a preview that changes nothing reads %q", same.Result())
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
