package transform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	before := treeHash()
	code := m.Run()
	if after := treeHash(); after != before {
		fmt.Printf("the package tree changed while the tests ran: %s became %s\n", before, after)
		os.Exit(1)
	}
	os.Exit(code)
}

func treeHash() string {
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

const (
	censusTurns          = 17
	v2MaintenanceSession = "turn-18d6a27c7dfb1644.json"
)

var measuredElsewhere = map[string]string{
	v2MaintenanceSession:         "the v2 maintenance session, measured on its own against testdata/v2-before",
	"turn-18d6a5df2caeac68.json": "recorded after this census was frozen, and it carries no write call",
}

func load(t *testing.T) ([]Write, int) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join("..", "stopcheck", "corpus", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	names = slices.DeleteFunc(names, func(name string) bool {
		_, elsewhere := measuredElsewhere[filepath.Base(name)]
		return elsewhere
	})
	for name, why := range measuredElsewhere {
		t.Logf("outside this census: %s, %s", name, why)
	}
	writes, turns, err := LoadFiles(names, filepath.Join("testdata", "before"))
	if err != nil {
		t.Fatal(err)
	}
	if turns != censusTurns {
		t.Fatalf("the census is %d turns of the shared corpus and this run read %d, so a recording arrived or left and every figure below moves with it", censusTurns, turns)
	}
	if len(writes) < 10 {
		t.Fatalf("the recorded turns held %d write calls, the measurement needs at least ten", len(writes))
	}
	return writes, turns
}

func TestBothArmsOverTheRecordedWrites(t *testing.T) {
	writes, turns := load(t)
	rows, err := Measure(writes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	report := Report(rows, FitTokens(writes), turns)
	t.Log("\n" + report)

	var creates, replaces, refused int
	for _, row := range rows {
		switch {
		case !row.Expressible:
			refused++
		case row.Before == "":
			creates++
		default:
			replaces++
		}
	}
	if creates+replaces+refused != len(rows) {
		t.Fatalf("the census counted %d rows out of %d", creates+replaces+refused, len(rows))
	}
	if creates != 11 || replaces != 3 || refused != 0 {
		t.Fatalf("the recorded corpus is 11 creates, 3 replaces and 0 refusals, this run counted %d, %d and %d",
			creates, replaces, refused)
	}
	t.Logf("%d writes: %d created a file, %d changed one that existed, %d could not be expressed",
		len(rows), creates, replaces, refused)
}

func TestEveryExpressedEditRebuildsTheRecordedFileExactly(t *testing.T) {
	writes, _ := load(t)
	rows, err := Measure(writes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if !row.Expressible && row.Why == "" {
			t.Fatalf("%s step %d, %s was refused with no reason given", row.Session, row.Step, row.Path)
		}
		if row.Expressible && row.Shape == "" {
			t.Fatalf("%s step %d, %s was expressed with no shape", row.Session, row.Step, row.Path)
		}
	}
}

func TestOneLineChangedInTheLargestRecordedFile(t *testing.T) {
	writes, _ := load(t)
	largest := writes[0]
	for _, write := range writes {
		if len(write.Content) > len(largest.Content) {
			largest = write
		}
	}
	lines := strings.SplitAfter(largest.Content, "\n")
	changed := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" && strings.Count(largest.Content, line) == 1 {
			changed = i
			break
		}
	}
	if changed < 0 {
		t.Fatal("the largest recorded file has no line that appears exactly once")
	}
	after := append(append([]string{}, lines[:changed]...), "const benchMarker = 1\n")
	after = append(after, lines[changed+1:]...)

	one := Write{
		Session: largest.Session, Step: largest.Step, Path: largest.Path,
		Before: largest.Content, Content: strings.Join(after, ""),
		ResultBytes: largest.ResultBytes, CompletionTokens: largest.CompletionTokens, AloneInStep: true,
	}
	rows, err := Measure([]Write{one}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !rows[0].Expressible {
		t.Fatalf("one line changed in %s could not be expressed: %s", one.Path, rows[0].Why)
	}
	fit := FitTokens(writes)
	t.Logf("one line changed in %s (%d bytes): write arm out %d tokens in %d, typed arm out %d tokens in %d",
		one.Path, len(one.Before),
		fit.Tokens(rows[0].WriteOut), fit.Tokens(rows[0].WriteIn),
		fit.Tokens(rows[0].TypedOut), fit.Tokens(rows[0].TypedIn))
	if fit.Tokens(rows[0].TypedOut) >= fit.Tokens(rows[0].WriteOut) {
		t.Fatal("on a file this size the typed edit is not cheaper than rewriting it, which the mechanism exists to be")
	}
}

func TestTheFitComesFromTheRecordedStepsAlone(t *testing.T) {
	writes, _ := load(t)
	fit := FitTokens(writes)
	if fit.Points < 10 {
		t.Fatalf("the fit used %d points, too few to quote", fit.Points)
	}
	if fit.TokensPerByte <= 0 || fit.TokensPerByte > 1 {
		t.Fatalf("the fit gave %.4f tokens per byte, which is not a rate any tokeniser produces", fit.TokensPerByte)
	}
}
