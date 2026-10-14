package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	sampleEvery = 100 * time.Millisecond
	mebibyte    = 1 << 20
)

type member struct {
	PID, Parent      uint32
	Exe, CommandLine string
	Kind             string
	CPUS             float64
	PeakWorkingSetMB float64
	PeakPrivateWSMB  float64
	PeakPrivateMB    float64
}

type observation struct {
	root         uint32
	members      map[uint32]*member
	kindPeakMB   map[string]float64
	WallS        float64
	Exit         int
	JobCPUS      float64
	JobProcesses int
	PeakCommitMB float64
	PeakTreeMB   float64
	PeakSplit    map[string]float64
	Samples      int
}

type kindRow struct {
	Kind             string  `json:"kind"`
	Processes        int     `json:"processes"`
	AtTreePeakMB     float64 `json:"at_tree_peak_mb"`
	OwnPeakMB        float64 `json:"own_peak_mb"`
	LargestOneMB     float64 `json:"largest_one_process_mb"`
	LargestOneWSMB   float64 `json:"largest_one_process_private_working_set_mb"`
	LargestPrivateMB float64 `json:"largest_one_process_private_mb"`
	CPUS             float64 `json:"cpu_s"`
}

type run struct {
	Label         string    `json:"label"`
	Command       []string  `json:"command"`
	Version       string    `json:"version"`
	Commit        string    `json:"commit"`
	Model         string    `json:"model"`
	Credential    string    `json:"credential"`
	Wire          string    `json:"wire"`
	Machine       string    `json:"machine"`
	Date          string    `json:"date"`
	Method        string    `json:"method"`
	Exit          int       `json:"exit"`
	WallS         float64   `json:"wall_s"`
	JobCPUS       float64   `json:"job_cpu_s"`
	SeenCPUS      float64   `json:"seen_cpu_s"`
	JobProcesses  int       `json:"job_processes"`
	SeenProcesses int       `json:"seen_processes"`
	PeakTreeMB    float64   `json:"peak_tree_rss_mb"`
	PeakCommitMB  float64   `json:"peak_commit_mb"`
	Samples       int       `json:"samples"`
	Kinds         []kindRow `json:"kinds"`
	Changed       string    `json:"changed,omitempty"`
}

var shells = []string{"bash", "sh", "cmd", "powershell", "pwsh"}

func exeBase(exe string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
}

func (o *observation) add(m *member) {
	o.members[m.PID] = m
	m.Kind = o.kindOf(m)
}

func (o *observation) kindOf(m *member) string {
	if m.PID == o.root {
		return "tofu"
	}
	underShell := false
	for p := m; p != nil && p.PID != o.root; p = o.members[p.Parent] {
		switch base := exeBase(p.Exe); {
		case base == "go":
			return "go " + goVerb(p.CommandLine)
		case base == "gofmt":
			return "gofmt"
		case slices.Contains(shells, base):
			underShell = true
		}
	}
	if underShell {
		return "shells"
	}
	return exeBase(m.Exe)
}

func goVerb(line string) string {
	words := splitCommandLine(line)
	for _, word := range words[min(1, len(words)):] {
		if !strings.HasPrefix(word, "-") {
			return word
		}
	}
	return "(no verb)"
}

func (o *observation) record(total float64, split map[string]float64) {
	if total > o.PeakTreeMB {
		o.PeakTreeMB, o.PeakSplit = total, split
	}
	for kind, mb := range split {
		o.kindPeakMB[kind] = max(o.kindPeakMB[kind], mb)
	}
}

func (o observation) kinds() ([]kindRow, float64) {
	byKind := map[string]*kindRow{}
	var seenCPU float64
	for _, m := range o.members {
		k := byKind[m.Kind]
		if k == nil {
			k = &kindRow{Kind: m.Kind, AtTreePeakMB: o.PeakSplit[m.Kind], OwnPeakMB: o.kindPeakMB[m.Kind]}
			byKind[m.Kind] = k
		}
		k.Processes++
		k.CPUS += m.CPUS
		k.LargestOneMB = max(k.LargestOneMB, m.PeakWorkingSetMB)
		k.LargestOneWSMB = max(k.LargestOneWSMB, m.PeakPrivateWSMB)
		k.LargestPrivateMB = max(k.LargestPrivateMB, m.PeakPrivateMB)
		seenCPU += m.CPUS
	}
	var rows []kindRow
	for _, kind := range slices.Sorted(maps.Keys(byKind)) {
		rows = append(rows, *byKind[kind])
	}
	return rows, seenCPU
}

func here() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}

func repoRoot() string {
	return filepath.Dir(filepath.Dir(here()))
}

func stage(args []string) error {
	if len(args) != 1 {
		return errors.New("stage needs the directory the bench/rivals seed is copied into")
	}
	seed := filepath.Join(repoRoot(), "bench", "rivals", "testdata", "seed")
	entries, err := os.ReadDir(seed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(args[0], 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(seed, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(args[0], entry.Name()), body, 0o644); err != nil {
			return err
		}
	}
	for _, git := range [][]string{{"init", "-q"}, {"config", "core.autocrlf", "false"}, {"add", "-A"},
		{"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", args[0]}, git...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", git, err, out)
		}
	}
	return nil
}

func benchEnv(goDir, home, cassette, pprof string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		dropped := strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
		goOwned := goDir != "" && (name == "GOCACHE" || name == "GOTMPDIR" || name == "GOFLAGS")
		homeOwned := home != "" && (name == "HOME" || name == "USERPROFILE")
		return dropped || goOwned || homeOwned || name == "TOFU_DRIVE_CASSETTE" || name == "TOFU_PPROF"
	})
	if goDir != "" {
		env = append(env, "GOCACHE="+filepath.Join(goDir, "cache"), "GOTMPDIR="+filepath.Join(goDir, "tmp"), "GOFLAGS=-p=2")
	}
	if home != "" {
		env = append(env, "HOME="+home, "USERPROFILE="+home)
	}
	if cassette != "" {
		env = append(env, "TOFU_DRIVE_CASSETTE="+cassette)
	}
	if pprof != "" {
		env = append(env, "TOFU_PPROF="+pprof)
	}
	return env
}

func sample(args []string) error {
	flags := flag.NewFlagSet("sample", flag.ExitOnError)
	label := flags.String("label", "", "the run's name, and its file under runs/")
	dir := flags.String("dir", "", "the project the command runs in")
	profile := flags.String("profile", "", "where stdout.txt and members.json are written, and with -pprof tofu's cpu.pprof, heap-peak.pprof, heap-end.pprof and memstats.json")
	pprof := flags.Bool("pprof", false, "set TOFU_PPROF to -profile, so tofu profiles itself")
	goDir := flags.String("go", "", "a fresh Go cache and temp root, as bench/rivals gives each run")
	home := flags.String("home", "", "a fresh HOME, empty for the real one a live login needs")
	cassette := flags.String("cassette", "", "a scripted reply deck, so no model is called")
	credential := flags.String("credential", "subscription", "subscription or none")
	wire := flags.String("wire", "", "the wire tofu spoke")
	_ = flags.Parse(args)
	command := flags.Args()
	if *label == "" || *dir == "" || *profile == "" || len(command) == 0 {
		return errors.New("sample needs -label, -dir, -profile and a command after --")
	}
	if *goDir != "" {
		for _, made := range []string{filepath.Join(*goDir, "cache"), filepath.Join(*goDir, "tmp")} {
			if err := os.MkdirAll(made, 0o755); err != nil {
				return err
			}
		}
	}
	if err := os.MkdirAll(*profile, 0o755); err != nil {
		return err
	}
	pprofDir := ""
	if *pprof {
		pprofDir = *profile
	}
	env := benchEnv(*goDir, *home, *cassette, pprofDir)
	build, _ := exec.Command(command[0], "version").Output()
	fields := map[string]string{}
	for _, line := range strings.Split(string(build), "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		fields[name] = strings.TrimSpace(value)
	}
	transcript, err := os.Create(filepath.Join(*profile, "stdout.txt"))
	if err != nil {
		return err
	}
	defer func() { _ = transcript.Close() }()
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = *dir, env, transcript, transcript
	o, err := observe(cmd)
	if err != nil {
		return err
	}
	kinds, seenCPU := o.kinds()
	members, _ := json.MarshalIndent(slices.SortedFunc(maps.Values(o.members), func(a, b *member) int { return int(a.PID) - int(b.PID) }), "", " ")
	if err := os.WriteFile(filepath.Join(*profile, "members.json"), members, 0o644); err != nil {
		return err
	}
	said, _ := os.ReadFile(transcript.Name())
	var models []string
	for _, match := range regexp.MustCompile(` model (\S+) asked_as`).FindAllStringSubmatch(string(said), -1) {
		if !slices.Contains(models, match[1]) {
			models = append(models, match[1])
		}
	}
	changed, _ := exec.Command("git", "-C", *dir, "diff", "--shortstat").Output()
	shown := []string{exeBase(command[0])}
	for _, word := range command[1:] {
		shown = append(shown, strings.ReplaceAll(word, *dir, "<project>"))
	}
	r := run{Label: *label, Command: shown, Version: fields["version"], Commit: fields["commit"],
		Model: strings.Join(models, "+"), Credential: *credential, Wire: *wire,
		Machine: machine(), Date: time.Now().Format("2006-01-02 15:04"),
		Method: treeMethod, Exit: o.Exit, WallS: o.WallS, JobCPUS: o.JobCPUS, SeenCPUS: seenCPU, JobProcesses: o.JobProcesses, SeenProcesses: len(o.members),
		PeakTreeMB: o.PeakTreeMB, PeakCommitMB: o.PeakCommitMB, Samples: o.Samples, Kinds: kinds, Changed: strings.TrimSpace(string(changed))}
	if err := os.MkdirAll(filepath.Join(here(), "runs"), 0o755); err != nil {
		return err
	}
	body, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(here(), "runs", *label+".json"), append(body, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Print(r.table())
	return nil
}

func (r run) table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: tofu %s commit %s, exit %d, %.1f s wall, job CPU %.2f s (%.2f s seen per process), peak tree RSS %.1f MB, peak commit %.1f MB, %d of %d processes seen, %d samples, model %s, %s\n\n",
		r.Label, cmp.Or(r.Version, "not recorded"), cmp.Or(r.Commit, "not recorded"), r.Exit, r.WallS, r.JobCPUS, r.SeenCPUS, r.PeakTreeMB, r.PeakCommitMB, r.SeenProcesses, r.JobProcesses, r.Samples, r.Model, r.Changed)
	b.WriteString("| kind | processes | MB at the tree peak | kind's own peak MB | largest one process MB | of it private MB | largest commit MB | CPU s |\n|---|---|---|---|---|---|---|---|\n")
	for _, k := range r.Kinds {
		fmt.Fprintf(&b, "| %s | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.2f |\n", k.Kind, k.Processes, k.AtTreePeakMB, k.OwnPeakMB, k.LargestOneMB, k.LargestOneWSMB, k.LargestPrivateMB, k.CPUS)
	}
	return b.String()
}

func report([]string) error {
	paths, err := filepath.Glob(filepath.Join(here(), "runs", "*.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r run
		if err := json.Unmarshal(body, &r); err != nil {
			return err
		}
		fmt.Printf("%s\n%s, %s credential, wire %s, %s\n\n", r.table(), r.Date, r.Credential, r.Wire, r.Machine)
	}
	return nil
}

func main() {
	verbs := map[string]func([]string) error{"stage": stage, "sample": sample, "report": report, "size": sizes, "idle": idle, "idles": idles}
	if len(os.Args) < 2 || verbs[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: profile stage|sample|report|size|idle|idles")
		os.Exit(2)
	}
	if err := verbs[os.Args[1]](os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "bench/profile:", err)
		os.Exit(1)
	}
}
