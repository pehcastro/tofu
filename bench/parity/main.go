package main

import (
	"cmp"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

//go:embed cases drive serve inputs
var data embed.FS

type outcome string

const (
	outcomeEqual outcome = "equal"
	outcomeDiff  outcome = "diff"
	outcomeError outcome = "error"
)

type result struct {
	c       testCase
	outcome outcome
	detail  string
}

var unsafeInSlug = regexp.MustCompile(`[^a-z0-9]+`)

func main() {
	a := flag.String("a", "", "the baseline tofu executable")
	b := flag.String("b", "", "the tofu executable compared with it")
	runs := flag.String("runs", "", "the folder each run makes its homes and projects under; beside --a when empty")
	out := flag.String("out", "", "the report path; bench/parity/results/<stamp>.md when empty")
	only := flag.String("only", "", "run only the cases whose name or catalog file holds this text")
	flag.Parse()
	failed, err := run(*a, *b, *runs, *out, *only)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parity:", err)
		os.Exit(2)
	}
	if failed {
		os.Exit(1)
	}
}

func run(aExe, bExe, runs, out, only string) (bool, error) {
	if aExe == "" || bExe == "" {
		return false, fmt.Errorf("name both builds: --a baseline.exe --b candidate.exe")
	}
	started := time.Now()
	stamp := started.Format("20060102-150405")
	cat, err := readCatalog(data)
	if err != nil {
		return false, err
	}
	if only != "" {
		cat.cases = slices.DeleteFunc(cat.cases, func(c testCase) bool { return !strings.Contains(c.name+" "+c.from, only) })
	}
	root := filepath.Join(cmp.Or(runs, filepath.Join(filepath.Dir(mustAbs(aExe)), "runs")), stamp)
	if exists(root) {
		return false, fmt.Errorf("%s is already a run; start the next one a second later", root)
	}
	dataDir := filepath.Join(root, "data")
	if err := unpack(dataDir); err != nil {
		return false, err
	}
	w := newWork(filepath.Join(root, "w"), dataDir)
	sides := make([]build, 2)
	for i, exe := range []string{aExe, bExe} {
		if sides[i], err = describe([]string{"a", "b"}[i], mustAbs(exe), w, root); err != nil {
			return false, err
		}
	}
	var results []result
	for i, c := range cat.cases {
		r := compare(c, sides, w, filepath.Join(root, fmt.Sprintf("%03d-%s", i+1, strings.Trim(unsafeInSlug.ReplaceAllString(strings.ToLower(c.name), "-"), "-"))))
		results = append(results, r)
		fmt.Printf("[%d/%d] %s %s\n", i+1, len(cat.cases), r.outcome, c.name)
	}
	report := renderReport(sides, cat, results)
	if out == "" {
		out = filepath.Join("bench", "parity", "results", stamp+".md")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(out, []byte(report), 0o644); err != nil {
		return false, err
	}
	equal := 0
	for _, r := range results {
		if r.outcome == outcomeEqual {
			equal++
		}
	}
	fmt.Printf("%d/%d equal, %d manual, %d unreachable, %s, %s on %s %s/%s\nreport %s\nruns %s\n", equal, len(results), len(cat.manual), len(cat.unreachable),
		time.Since(started).Round(time.Second), started.Format(time.DateOnly), hostname(), runtime.GOOS, runtime.GOARCH, out, root)
	return equal != len(results), nil
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}
	return abs
}

func hostname() string {
	name, _ := os.Hostname()
	return name
}

func unpack(dir string) error {
	return fs.WalkDir(data, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := data.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}

func describe(label, exe string, w work, root string) (build, error) {
	sum, err := fileHash(exe)
	if err != nil {
		return build{}, err
	}
	if err := w.make(); err != nil {
		return build{}, err
	}
	raw, err := w.command(exe, []string{"version", "--json"}, nil, "").Output()
	if err != nil {
		return build{}, fmt.Errorf("%s version --json: %w", exe, err)
	}
	if err := os.RemoveAll(w.root); err != nil {
		return build{}, err
	}
	var envelope struct {
		Data struct{ Version, Go string } `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return build{}, fmt.Errorf("%s version --json: %w", exe, err)
	}
	version := envelope.Data
	plain, _, _ := strings.Cut(version.Version, "+")
	var literals [][2]string
	for _, path := range [][2]string{{w.project, "PROJECT"}, {w.home, "HOME"}, {w.hostTemp, "HOSTTEMP"}, {w.data, "DATA"}, {w.root, "WORK"}, {root, "RUNS"}, {exe, "EXE"}} {
		literals = append(literals, pathForms(path[0], path[1])...)
	}
	literals = append(literals, [2]string{version.Version, "VERSION"}, [2]string{plain, "VERSION"}, [2]string{version.Go, "GOVERSION"})
	return build{label: label, exe: exe, sum: sum, literals: literals}, nil
}

func fileHash(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func compare(c testCase, sides []build, w work, kept string) result {
	var seen [2]normalized
	var files [2]string
	for i, side := range sides {
		got, err := runSide(side, c, w, filepath.Join(kept, side.label))
		if err != nil {
			_ = os.RemoveAll(w.root)
			return result{c: c, outcome: outcomeError, detail: side.label + ": " + err.Error()}
		}
		mask := newMasker(side.literals)
		seen[i] = mask.apply(got.transcript)
		if err := os.WriteFile(filepath.Join(kept, side.label+".txt"), []byte(seen[i].text), 0o644); err != nil {
			return result{c: c, outcome: outcomeError, detail: side.label + ": " + err.Error()}
		}
		listed := make([]string, len(got.files))
		for j, file := range got.files {
			listed[j] = mask.unseen(file)
		}
		slices.Sort(listed)
		files[i] = strings.Join(listed, "\n")
	}
	if err := seen[0].agrees(seen[1]); err != nil {
		return result{c: c, outcome: outcomeDiff, detail: err.Error() + "\n" + window(seen[0].text, seen[1].text)}
	}
	if files[0] != files[1] {
		return result{c: c, outcome: outcomeDiff, detail: "the files written differ\n" + window(files[0], files[1])}
	}
	return result{c: c, outcome: outcomeEqual}
}
