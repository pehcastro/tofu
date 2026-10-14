package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

type wallRow struct {
	Version      string
	Commit       string
	Session      string
	Arm          string
	Agents       int
	Run          int
	Agent        string
	Model        string
	Credential   string
	Machine      string
	Date         string
	Answered     int
	Exit         int
	SpawnMS      float64
	NoticeParts  map[string]float64
	StartMS      float64
	Requests     int
	ModelMS      float64
	ToolsMS      float64
	BetweenMS    float64
	GatherMS     float64
	LeadMS       float64
	ReadMS       float64
	BeforeMS     float64
	StartupMS    float64
	AskMS        float64
	FanMS        float64
	TailMS       float64
	MemoryMS     float64
	WallMS       float64
	LeadTurns    int
	LeadRequests int
}

type tracedCall struct {
	Call       string  `json:"call"`
	Agent      string  `json:"agent"`
	Request    string  `json:"request"`
	Tool       string  `json:"tool"`
	DurationMS float64 `json:"duration_ms"`
}

type tracedInsert struct {
	Turn    string    `json:"turn"`
	Origin  string    `json:"origin"`
	TakenAt time.Time `json:"taken_at"`
	Text    string    `json:"text"`
}

type traced struct {
	Agents []struct {
		Agent     string    `json:"agent"`
		SpawnCall string    `json:"spawn_call"`
		StartedAt time.Time `json:"started_at"`
		EndedAt   time.Time `json:"ended_at"`
	} `json:"agents"`
	Requests []struct {
		Request    string  `json:"request"`
		Agent      string  `json:"agent"`
		Turn       string  `json:"turn"`
		Model      string  `json:"model"`
		DurationMS float64 `json:"duration_ms"`
	} `json:"requests"`
	Calls    []tracedCall   `json:"calls"`
	Inserted []tracedInsert `json:"inserted"`
	Notices  []struct {
		Text string `json:"text"`
	} `json:"notices"`
	Messages []struct {
		Turn string    `json:"turn"`
		Role string    `json:"role"`
		At   time.Time `json:"at"`
	} `json:"messages"`
	Memory []struct {
		Request string `json:"request"`
	} `json:"memory"`
}

type listedSession struct {
	ID string    `json:"id"`
	At time.Time `json:"at"`
}

type requestDetail struct {
	At         time.Time `json:"at"`
	DurationMS float64   `json:"duration_ms"`
}

func (d requestDetail) end() time.Time {
	return d.At.Add(time.Duration(d.DurationMS * float64(time.Millisecond)))
}

type sessionTimes struct {
	trace   traced
	details map[string]requestDetail
}

type tofuCLI struct {
	binary, project string
	env             []string
}

func (t tofuCLI) data(into any, args ...string) error {
	cmd := exec.Command(t.binary, append(args, "--json")...)
	cmd.Dir, cmd.Env = t.project, t.env
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("tofu %s: %w", strings.Join(args, " "), err)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return err
	}
	return json.Unmarshal(envelope.Data, into)
}

func (t tofuCLI) version() (string, string) {
	cmd := exec.Command(t.binary, "version")
	cmd.Env = t.env
	out, _ := cmd.Output()
	fields := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		fields[name] = strings.TrimSpace(value)
	}
	return fields["version"], fields["commit"]
}

func (t tofuCLI) newest(since time.Time) (string, error) {
	var listed struct {
		Sessions []listedSession `json:"sessions"`
	}
	if err := t.data(&listed, "session", "list"); err != nil {
		return "", err
	}
	at := slices.IndexFunc(listed.Sessions, func(s listedSession) bool { return !s.At.Before(since) })
	if at < 0 {
		return "", errors.New("no session began after the run started")
	}
	return listed.Sessions[at].ID, nil
}

func (t tofuCLI) times(session string) (sessionTimes, error) {
	got := sessionTimes{details: map[string]requestDetail{}}
	if err := t.data(&got.trace, "session", "trace", session); err != nil {
		return got, err
	}
	var ids []string
	for _, request := range got.trace.Requests {
		ids = append(ids, request.Request)
	}
	for _, request := range got.trace.Memory {
		ids = append(ids, request.Request)
	}
	for _, id := range ids {
		var detail requestDetail
		if err := t.data(&detail, "session", "request", session, id); err != nil {
			return got, err
		}
		got.details[id] = detail
	}
	return got, nil
}

var noticeWords = regexp.MustCompile(`spawn of (\S+) took (\S+) before it ran: (.*)`)

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func (s sessionTimes) noticeSteps() map[string]map[string]float64 {
	steps := map[string]map[string]float64{}
	for _, notice := range s.trace.Notices {
		match := noticeWords.FindStringSubmatch(notice.Text)
		if match == nil {
			continue
		}
		total, _ := time.ParseDuration(match[2])
		parts := map[string]float64{"total": ms(total)}
		for _, part := range strings.Split(match[3], ", ") {
			at := strings.LastIndex(part, " ")
			took, _ := time.ParseDuration(part[at+1:])
			parts[part[:at]] = ms(took)
		}
		steps[match[1]] = parts
	}
	return steps
}

func (s sessionTimes) askEnd() time.Time {
	at := slices.IndexFunc(s.trace.Calls, func(call tracedCall) bool { return call.Tool == "spawn" })
	if at < 0 {
		return time.Time{}
	}
	return s.details[s.trace.Calls[at].Request].end()
}

func (s sessionTimes) rows(began, ended time.Time, base wallRow) []wallRow {
	turnEnd := map[string]time.Time{}
	var models []string
	leadBegan := ended
	for _, request := range s.trace.Requests {
		if !slices.Contains(models, request.Model) {
			models = append(models, request.Model)
		}
		if request.Agent == "" {
			base.LeadRequests++
			turnEnd[request.Turn] = later(turnEnd[request.Turn], s.details[request.Request].end())
			if s.details[request.Request].At.Before(leadBegan) {
				leadBegan = s.details[request.Request].At
			}
		}
	}
	for _, message := range s.trace.Messages {
		if message.Role == "assistant" {
			turnEnd[message.Turn] = later(turnEnd[message.Turn], message.At)
		}
	}
	var final time.Time
	for _, end := range turnEnd {
		final = later(final, end)
	}
	for _, request := range s.trace.Memory {
		if detail := s.details[request.Request]; !detail.At.Before(final) {
			base.MemoryMS += detail.DurationMS
		}
	}
	base.Model, base.LeadTurns, base.StartupMS, base.TailMS = strings.Join(models, "+"), len(turnEnd), ms(leadBegan.Sub(began)), ms(ended.Sub(final))
	steps := s.noticeSteps()
	var rows []wallRow
	for _, agent := range s.trace.Agents {
		measured := base
		measured.Agent = agent.Agent
		var spawnedAt, first time.Time
		for _, call := range s.trace.Calls {
			if call.Call == agent.SpawnCall {
				spawnedAt, measured.AskMS = s.details[call.Request].end(), s.details[call.Request].DurationMS
			}
			if call.Agent == agent.Agent {
				measured.ToolsMS += call.DurationMS
			}
		}
		for _, request := range s.trace.Requests {
			if request.Agent != agent.Agent {
				continue
			}
			measured.Requests++
			measured.ModelMS += request.DurationMS
			if at := s.details[request.Request].At; first.IsZero() || at.Before(first) {
				first = at
			}
		}
		measured.NoticeParts = steps[agent.Agent]
		measured.SpawnMS = ms(agent.StartedAt.Sub(spawnedAt))
		measured.StartMS = ms(first.Sub(agent.StartedAt))
		measured.BetweenMS = ms(agent.EndedAt.Sub(first)) - measured.ModelMS - measured.ToolsMS
		readIn := slices.IndexFunc(s.trace.Inserted, func(item tracedInsert) bool {
			return item.Origin == "sub-agent report" && strings.Contains(item.Text, "sub-agent "+agent.Agent+" is ")
		})
		if readIn >= 0 {
			item := s.trace.Inserted[readIn]
			measured.GatherMS = ms(item.TakenAt.Sub(agent.EndedAt))
			measured.LeadMS = ms(turnEnd[item.Turn].Sub(item.TakenAt))
			measured.ReadMS = ms(turnEnd[item.Turn].Sub(spawnedAt))
		}
		measured.BeforeMS, measured.FanMS = ms(spawnedAt.Sub(began)), ms(final.Sub(spawnedAt))
		rows = append(rows, measured)
	}
	if len(rows) == 0 {
		rows = append(rows, base)
	}
	return rows
}

func appendWallRows(path string, rows []wallRow) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, r := range rows {
		line, _ := json.Marshal(r)
		if _, err := file.Write(append(line, '\n')); err != nil {
			return errors.Join(err, file.Close())
		}
		fmt.Printf("%s %s agents=%d run=%d %s wait=%.0f model=%.0f/%d gather=%.0f lead=%.0f read=%.0f startup=%.0f ask=%.0f memory=%.0f tail=%.0f wall=%.0f answered=%d\n",
			r.Version, r.Arm, r.Agents, r.Run, r.Agent, r.SpawnMS-r.NoticeParts["total"], r.ModelMS, r.Requests, r.GatherMS, r.LeadMS, r.ReadMS, r.StartupMS, r.AskMS, r.MemoryMS, r.TailMS, r.WallMS, r.Answered)
	}
	return file.Close()
}

func wall(args []string) error {
	flags := flag.NewFlagSet("wall", flag.ExitOnError)
	var base wallRow
	flags.StringVar(&base.Arm, "arm", "", "cassette or live")
	flags.IntVar(&base.Agents, "agents", 0, "sub-agents asked for")
	flags.IntVar(&base.Run, "run", 1, "which run of this rung")
	flags.StringVar(&base.Credential, "credential", "subscription", "subscription or none")
	binary := flags.String("tofu", "", "the tofu binary measured")
	project := flags.String("project", "", "the scratch project holding f0.txt to f10.txt")
	home := flags.String("home", "", "a fresh HOME for the run, the caller's when empty")
	out := flags.String("out", "", "the file rows are appended to")
	transcript := flags.String("transcript", "", "where tofu's output is kept")
	_ = flags.Parse(args)
	if *binary == "" || *project == "" || *out == "" || *transcript == "" || flags.NArg() == 0 {
		return errors.New("wall needs -tofu, -project, -out, -transcript and the tofu run arguments after --")
	}
	cli := tofuCLI{binary: *binary, project: *project, env: os.Environ()}
	if *home != "" {
		cli.env = append(cli.env, "HOME="+*home, "USERPROFILE="+*home)
	}
	base.Version, base.Commit = cli.version()
	cmd := exec.Command(*binary, flags.Args()...)
	var said bytes.Buffer
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = *project, cli.env, &said, &said
	began := time.Now()
	_ = cmd.Run()
	ended := time.Now()
	base.Exit, base.WallMS, base.Machine, base.Date = cmd.ProcessState.ExitCode(), ms(ended.Sub(began)), machine(), began.Format(time.DateOnly)
	if err := os.WriteFile(*transcript, said.Bytes(), 0o644); err != nil {
		return err
	}
	for _, line := range firstLines(*project, base.Agents) {
		if strings.Contains(said.String(), line) {
			base.Answered++
		}
	}
	session, err := cli.newest(began)
	if err != nil {
		return err
	}
	times, err := cli.times(session)
	if err != nil {
		return err
	}
	base.Session = session
	return appendWallRows(*out, times.rows(began, ended, base))
}

func retrace(args []string) error {
	flags := flag.NewFlagSet("retrace", flag.ExitOnError)
	binary := flags.String("tofu", "", "the tofu binary that reads the sessions")
	project := flags.String("project", "", "the project the sessions ran in")
	from := flags.String("from", "", "rows written before a column existed, one run each, in session order")
	sessions := flags.String("sessions", "", "the session ids of those runs, comma separated, in the same order")
	out := flags.String("out", "", "the file the rows are appended to")
	_ = flags.Parse(args)
	groups, err := readWallRows(*from)
	if err != nil {
		return err
	}
	var runs []wallRow
	for _, rows := range groups {
		for _, r := range rows {
			if len(runs) == 0 || runs[len(runs)-1].Run != r.Run || runs[len(runs)-1].Agents != r.Agents {
				runs = append(runs, r)
			}
		}
	}
	ids := strings.Split(*sessions, ",")
	if len(ids) != len(runs) {
		return fmt.Errorf("%d runs in %s and %d sessions given", len(runs), *from, len(ids))
	}
	cli := tofuCLI{binary: *binary, project: *project, env: os.Environ()}
	for i, old := range runs {
		times, err := cli.times(ids[i])
		if err != nil {
			return err
		}
		began := times.askEnd().Add(-time.Duration(old.BeforeMS * float64(time.Millisecond)))
		base := wallRow{Version: old.Version, Commit: old.Commit, Session: ids[i], Arm: old.Arm, Agents: old.Agents, Run: old.Run, Credential: old.Credential,
			Machine: old.Machine, Date: old.Date, Answered: old.Answered, Exit: old.Exit, WallMS: old.WallMS}
		if err := appendWallRows(*out, times.rows(began, began.Add(time.Duration(old.WallMS*float64(time.Millisecond))), base)); err != nil {
			return err
		}
	}
	return nil
}
