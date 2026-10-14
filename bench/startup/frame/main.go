package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	composerPlaceholder = "Ask tofu to build"
	resumedNote         = ", carrying "
	typedProbe          = "zqxjv"
	frameDeadline       = 60 * time.Second
	mockLoginCode       = "mock-login-code"
	againSuffix         = "-again"
)

type arm struct {
	label, exe string
}

type row struct {
	Case       string   `json:"case"`
	Arm        string   `json:"arm"`
	Run        int      `json:"run"`
	FrameMS    float64  `json:"frame_ms"`
	EchoMS     float64  `json:"echo_ms"`
	ReadyMS    float64  `json:"ready_ms"`
	Events     int      `json:"events"`
	EventBytes int64    `json:"event_bytes"`
	AskedFirst []string `json:"asked_before_frame"`
	Version    string   `json:"version"`
	Commit     string   `json:"commit"`
	Credential string   `json:"credential"`
	Wire       string   `json:"wire"`
	Machine    string   `json:"machine"`
	Date       string   `json:"date"`
	Error      string   `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: frame measure|report [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "measure":
		err = measure(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	default:
		err = fmt.Errorf("unknown verb %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "frame:", err)
		os.Exit(1)
	}
}

func freshEnv(home string, extra ...string) []string {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		return name == "HOME" || name == "USERPROFILE" || strings.HasPrefix(name, "TOFU_") || strings.HasPrefix(name, "CLAUDE") ||
			strings.HasPrefix(name, "OPENROUTER") || strings.HasSuffix(name, "_API_KEY")
	})
	return append(append(env, "HOME="+home, "USERPROFILE="+home), extra...)
}

func gitProject(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# startup bench project\n"), 0o644); err != nil {
		return err
	}
	for _, git := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=bench", "-c", "user.email=bench@example.invalid", "commit", "-q", "-m", "seed"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, git...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v: %s", git, err, out)
		}
	}
	return nil
}

func login(exe, home string, env []string) error {
	cmd := exec.Command(exe, "login", "llm", "claude-sub", "--paste")
	cmd.Env, cmd.Stdin = env, strings.NewReader(mockLoginCode+"\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mock login in %s: %v: %s", home, err, out)
	}
	return nil
}

func versionOf(exe string) (string, error) {
	out, err := exec.Command(exe, "version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 2 {
		return "", fmt.Errorf("%s version: %v", exe, err)
	}
	return fields[1], nil
}

func parseArms(given string) ([]arm, error) {
	var arms []arm
	for _, one := range strings.Split(given, ",") {
		label, exe, found := strings.Cut(one, "=")
		if !found {
			return nil, fmt.Errorf("an arm is label=path, not %q", one)
		}
		arms = append(arms, arm{label: label, exe: exe})
	}
	return arms, nil
}

func measure(args []string) error {
	flags := flag.NewFlagSet("measure", flag.ExitOnError)
	armsFlag := flags.String("tofu", "", "label=path[,label=path], every arm runs each round, interleaved")
	work := flags.String("work", "", "a scratch folder for the fresh homes and projects")
	runs := flags.Int("runs", 10, "rounds per case and arm")
	cases := flags.String("cases", "fresh,resume", "fresh, resume, and either with -again: one unmeasured launch in the same home first, as a project opened before")
	out := flags.String("out", "", "the jsonl the rows are appended to")
	commit := flags.String("commit", "", "the commit the binaries were built from, since an archive build cannot say")
	stubDelay := flags.Duration("stub-delay", 0, "how long the stub takes to answer every request, as a slow network would")
	profile := flags.String("profile", "", "a folder each run's TOFU_PPROF and TOFU_BENCH_MARKS go under, written when ctrl+c twice quits the app after the measurement")
	_ = flags.Parse(args)
	arms, err := parseArms(*armsFlag)
	if err != nil || *work == "" || *out == "" || *commit == "" {
		return errors.Join(err, errors.New("measure needs -tofu, -work, -out and -commit"))
	}
	server, err := startStub(*stubDelay)
	if err != nil {
		return err
	}
	b := bench{work: *work, template: filepath.Join(*work, "template-home"), resumeProject: filepath.Join(*work, "resume-project"), server: server, profile: *profile}
	machine, _ := os.Hostname()
	machine += " " + runtime.GOOS + "/" + runtime.GOARCH + fmt.Sprintf(" %d cpu", runtime.NumCPU())
	versions := map[string]string{}
	for _, a := range arms {
		if versions[a.label], err = versionOf(a.exe); err != nil {
			return err
		}
		fmt.Printf("warm-up, discarded: %s version printed %s\n", a.label, versions[a.label])
	}
	if _, _, err := sessionSize(b.template); err != nil && strings.Contains(*cases, "resume") {
		if err := seedTemplate(arms[0].exe, b.template, b.resumeProject, server); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	for round := 1; round <= *runs; round++ {
		for _, kind := range strings.Split(*cases, ",") {
			for _, a := range arms {
				r := row{Case: kind, Arm: a.label, Run: round, Version: versions[a.label], Commit: *commit, Credential: "mock claude-sub login against a local stub",
					Wire: "none: no model call, the stub answers token and usage", Machine: machine, Date: time.Now().Format(time.RFC3339)}
				if err := b.once(&r, a.exe); err != nil {
					r.Error = err.Error()
				}
				line, _ := json.Marshal(r)
				if _, err := file.Write(append(line, '\n')); err != nil {
					return err
				}
				fmt.Printf("%s %s %d: frame %.0f ms, typed echo %.0f ms, ready %.0f ms %s\n", r.Case, r.Arm, r.Run, r.FrameMS, r.EchoMS, r.ReadyMS, r.Error)
			}
		}
	}
	return nil
}

func seedTemplate(exe, template, project string, server *stub) error {
	for _, dir := range []string{template, project} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := gitProject(project); err != nil {
		return err
	}
	if err := seedCorpus(exe, template, project, freshEnv(template, server.env()...)); err != nil {
		return err
	}
	events, size, err := sessionSize(template)
	fmt.Printf("seeded %d events, %d bytes of events.jsonl, in %s\n", events, size, template)
	return err
}

func sessionSize(home string) (int, int64, error) {
	found, err := filepath.Glob(filepath.Join(home, ".tofu", "projects", "*", "sessions", "*", "events.jsonl"))
	if err != nil || len(found) != 1 {
		return 0, 0, fmt.Errorf("want one seeded session under %s, found %d: %v", home, len(found), err)
	}
	body, err := os.ReadFile(found[0])
	return strings.Count(string(body), "\n"), int64(len(body)), err
}

type bench struct {
	work, template, resumeProject, profile string
	server                                 *stub
}

func (b bench) once(r *row, exe string) error {
	home, err := os.MkdirTemp(b.work, "home-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(home) }()
	env := freshEnv(home, b.server.env()...)
	kind, again := strings.CutSuffix(r.Case, againSuffix)
	project, command, markers := b.resumeProject, []string{exe, "--continue"}, []string{composerPlaceholder, resumedNote}
	switch kind {
	case "fresh":
		project, command, markers = filepath.Join(home, "project"), []string{exe}, []string{composerPlaceholder}
		if err := os.Mkdir(project, 0o755); err != nil {
			return err
		}
		if err := gitProject(project); err != nil {
			return err
		}
	case "resume":
		if err := os.CopyFS(home, os.DirFS(b.template)); err != nil {
			return err
		}
		if r.Events, r.EventBytes, err = sessionSize(home); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown case %q", r.Case)
	}
	if err := login(exe, home, env); err != nil {
		return err
	}
	if again {
		if err := b.open(&row{}, command, project, env, markers); err != nil {
			return fmt.Errorf("the unmeasured first launch: %w", err)
		}
	}
	if b.profile != "" {
		dir := filepath.Join(b.profile, fmt.Sprintf("%s-%s-%02d", r.Case, r.Arm, r.Run))
		env = append(env, "TOFU_PPROF="+dir, "TOFU_BENCH_MARKS="+dir+".marks")
	}
	return b.open(r, command, project, env, markers)
}

func (b bench) open(r *row, command []string, project string, env, markers []string) error {
	size := screenSize{columns: 120, rows: 40}
	c, started, err := launch(command, project, env, size)
	if err != nil {
		return err
	}
	defer c.close()
	defer c.quit()
	seen := watch(c, size)
	framed, err := seen.await(markers, started.Add(frameDeadline))
	if err != nil {
		return seen.failed(err)
	}
	r.FrameMS = float64(framed.Sub(started).Microseconds()) / 1000
	r.AskedFirst = b.server.between(started, framed)
	if _, err := c.Write([]byte(typedProbe)); err != nil {
		return err
	}
	echoed, err := seen.await([]string{typedProbe}, framed.Add(frameDeadline))
	if err != nil {
		return seen.failed(err)
	}
	r.EchoMS = float64(echoed.Sub(framed).Microseconds()) / 1000
	r.ReadyMS = r.FrameMS + r.EchoMS
	return nil
}
