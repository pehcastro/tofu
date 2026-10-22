package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tofu/internal/host"
	"tofu/internal/konst"
	"tofu/internal/session"
)

const (
	reportMarker      = "fanin report from"
	sourceReport      = "sub-agent report"
	sourceCheck       = "sub-agent check"
	sourceAsk         = "sub-agent gate ask"
	sourceEnvironment = "environment"
	roleUser          = "user"
	profileLimit      = 3 * time.Minute
	settleSlack       = 3 * time.Second
	askingHook        = `{"hooks":{"PreToolUse":[{"matcher":"glob","hooks":[{"type":"command","command":"echo '{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"permissionDecision\":\"ask\",\"permissionDecisionReason\":\"fanin asks the lead\"}}'"}]}]}}`
)

type profile struct {
	name      string
	finishing int
	settings  string
	hooks     string
}

type arm struct {
	label   string
	tofu    string
	version string
	commit  string
}

type measured struct {
	profile     profile
	arm         arm
	leadTurns   int
	reportTurns int
	reportsRead int
	checkTurns  int
	askTurns    int
	askWait     time.Duration
	reportWait  time.Duration
	lastRead    time.Duration
	failed      error
}

func parseArms(text string) ([]arm, error) {
	var arms []arm
	for _, pair := range strings.Split(text, ",") {
		label, path, named := strings.Cut(pair, "=")
		if !named || label == "" || path == "" {
			return nil, fmt.Errorf("--arms takes label=path pairs separated by commas, and was given %q", pair)
		}
		full, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		said, _ := exec.Command(full, "version").Output()
		version, commit := tofuBuild(string(said))
		arms = append(arms, arm{label: label, tofu: full, version: version, commit: commit})
	}
	return arms, nil
}

func tofuBuild(said string) (string, string) {
	fields := map[string]string{}
	for _, line := range strings.Split(said, "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		fields[name] = strings.TrimSpace(value)
	}
	return fields["version"], fields["commit"]
}

func (a arm) build() string {
	return fmt.Sprintf("tofu %s commit %s", cmp.Or(a.version, "not recorded"), cmp.Or(fmt.Sprintf("%.8s", a.commit), "not recorded"))
}

func drive(p profile, a arm, cassettes, work string) measured {
	m := measured{profile: p, arm: a}
	dir := filepath.Join(work, a.label, p.name)
	home, project := filepath.Join(dir, "home"), filepath.Join(dir, "project")
	files := map[string]string{filepath.Join(project, ".tofu", "settings.json"): cmp.Or(p.settings, "{}")}
	if p.hooks != "" {
		files[filepath.Join(home, ".tofu", "hooks.json")] = p.hooks
	}
	for path, body := range files {
		if m.failed = os.MkdirAll(filepath.Dir(path), 0o755); m.failed != nil {
			return m
		}
		if m.failed = os.WriteFile(path, []byte(body), 0o644); m.failed != nil {
			return m
		}
	}
	env := append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
	cassette, err := filepath.Abs(filepath.Join(cassettes, p.name+".jsonl"))
	if err != nil {
		m.failed = err
		return m
	}
	s, err := serve(a.tofu, cassette, project, filepath.Join(dir, "serve.log"), env)
	if err != nil {
		m.failed = err
		return m
	}
	started := time.Now()
	var state host.SessionState
	m.failed = func() error {
		if err := s.call("initialize", map[string]string{"client": "bench-fanin"}, &json.RawMessage{}); err != nil {
			return err
		}
		if err := s.call("session.open", host.SessionOpenParams{Asking: host.AskingAuto}, &json.RawMessage{}); err != nil {
			return err
		}
		if err := s.call("session.state", struct{}{}, &state); err != nil {
			return err
		}
		if err := s.call("turn.send", host.TurnSendParams{Session: state.Session, Text: "fan the work out to the workers"}, &json.RawMessage{}); err != nil {
			return err
		}
		return s.settle(p.finishing, konst.ReportGatherQuietMillis*time.Millisecond+settleSlack, profileLimit)
	}()
	s.close()
	if m.failed != nil {
		m.failed = fmt.Errorf("%w, its log is %s", m.failed, filepath.Join(dir, "serve.log"))
		return m
	}
	var trace host.SessionTrace
	if m.failed = verb(a.tofu, project, env, &trace, "session", "trace", state.Session); m.failed == nil {
		m.count(trace.Inserted, started)
	}
	return m
}

func (m *measured) count(inserted []session.TracedInsert, started time.Time) {
	opened := map[string]string{}
	reported := map[string]bool{}
	for _, insert := range inserted {
		if insert.Agent != "" || insert.Role != roleUser {
			continue
		}
		if _, seen := opened[insert.Turn]; !seen && insert.Origin != sourceEnvironment {
			opened[insert.Turn] = insert.Origin
		}
		waited := time.Duration(0)
		if insert.PostedAt != nil && insert.TakenAt != nil {
			waited = insert.TakenAt.Sub(*insert.PostedAt)
		}
		if strings.Contains(insert.Origin, sourceReport) {
			reported[insert.Turn] = true
			m.reportsRead += strings.Count(insert.Text, reportMarker)
			m.reportWait = max(m.reportWait, waited)
			if insert.TakenAt != nil {
				m.lastRead = max(m.lastRead, insert.TakenAt.Sub(started))
			}
		}
		if strings.Contains(insert.Origin, sourceAsk) {
			m.askWait = max(m.askWait, waited)
		}
	}
	for _, origin := range opened {
		m.leadTurns++
		switch origin {
		case sourceCheck:
			m.checkTurns++
		case sourceAsk:
			m.askTurns++
		}
	}
	m.reportTurns = len(reported)
}

func verb(tofu, project string, env []string, into any, args ...string) error {
	cmd := exec.Command(tofu, append(args, "--json")...)
	cmd.Dir, cmd.Env = project, env
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tofu %v: %w", args, err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("tofu %v printed no json: %w", args, err)
	}
	return json.Unmarshal(envelope.Data, into)
}

func table(rows []measured) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%-10s %-7s %10s %12s %12s %11s %9s %13s %15s %9s  %s\n", "profile", "arm", "lead turns", "report turns", "reports read", "check turns", "ask turns", "ask wait max", "report wait max", "last read", "tofu")
	for _, r := range rows {
		if r.failed != nil {
			fmt.Fprintf(&out, "%-10s %-7s FAILED, nothing counted: %v\n", r.profile.name, r.arm.label, r.failed)
			continue
		}
		fmt.Fprintf(&out, "%-10s %-7s %10d %12d %9d/%-2d %11d %9d %13s %15s %9s  %s\n", r.profile.name, r.arm.label, r.leadTurns, r.reportTurns, r.reportsRead, r.profile.finishing,
			r.checkTurns, r.askTurns, r.askWait.Round(time.Millisecond), r.reportWait.Round(time.Millisecond), r.lastRead.Round(100*time.Millisecond), r.arm.build())
	}
	return out.String()
}

func main() {
	armsFlag := flag.String("arms", "", "label=path pairs, comma separated: each tofu binary is driven over every profile, such as before=old.exe,after=new.exe")
	only := flag.String("profiles", "", "comma separated profile names, every profile when empty")
	cassettes := flag.String("cassettes", filepath.Join("bench", "fanin", "testdata"), "the folder holding one cassette per profile")
	flag.Parse()
	arms, err := parseArms(*armsFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench/fanin:", err)
		os.Exit(2)
	}
	work, err := os.MkdirTemp("", "tofu-bench-fanin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench/fanin:", err)
		os.Exit(1)
	}
	fmt.Printf("bench/fanin %s, %s/%s, %d cpus\n", time.Now().Format(time.DateOnly), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	fmt.Printf("  credential  none: every arm drives tofu serve on a cassette in a fresh home under %s\n", work)
	fmt.Printf("  window      quiet %dms, cap %dms, as built into this bench; each binary carries its own\n", konst.ReportGatherQuietMillis, konst.ReportGatherCapMillis)
	for _, a := range arms {
		fmt.Printf("  %-11s %s (%s)\n", a.label, a.build(), a.tofu)
	}
	fmt.Println()
	var rows []measured
	for _, p := range []profile{
		{name: "clustered", finishing: 3},
		{name: "straggler", finishing: 4},
		{name: "spread", finishing: 3},
		{name: "ask", finishing: 3, hooks: askingHook},
		{name: "checkins", finishing: 2, settings: `{"subAgentCheckSeconds": 1}`},
	} {
		if *only != "" && !strings.Contains(","+*only+",", ","+p.name+",") {
			continue
		}
		for _, a := range arms {
			rows = append(rows, drive(p, a, *cassettes, work))
		}
	}
	fmt.Print(table(rows))
}
