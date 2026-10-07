package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/konst"
	"tofu/internal/learn"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/transport"
)

const (
	moment       = "15:04:05"
	yes          = "y"
	followedName = "followed"
	armCommand   = "claude"
	argmaxOfTwo  = 0.5
)

var nouls = []string{"corrects", "bears_on", "lacks_info"}

type labels struct {
	Nouls    map[string]bool
	Followed string
}

type window struct {
	Key   string
	Said  learn.Said
	State learn.WindowState
	Hand  labels
}

func main() {
	chain := flag.String("chain", "", "the last session of the chain, required")
	dir := flag.String("dir", ".", "the project whose sessions are read")
	file := flag.String("labels", "bench/learn/labels.txt", "the hand labels")
	dump := flag.Bool("dump", false, "print every window to label and stop")
	arm := flag.Bool("arm", true, "also run the one call per transcript arm on the person's claude subscription")
	flag.Parse()
	if *chain == "" {
		fmt.Fprintln(os.Stderr, "bench learn: --chain names the last session of a chain in --dir, and it has no default")
		os.Exit(2)
	}
	if err := bench(*chain, *dir, *file, *dump, *arm); err != nil {
		fmt.Fprintln(os.Stderr, "bench learn:", err)
		os.Exit(1)
	}
}

func bench(chain, dir, file string, dump, arm bool) error {
	store, err := session.OpenIn(dir)
	if err != nil {
		return err
	}
	headers, err := learn.Chain(store, chain)
	if err != nil {
		return err
	}
	run, err := learn.Scan([]learn.Source{{Store: store, Headers: headers}}, learn.Known{}, time.Now())
	if err != nil {
		return err
	}
	states := run.Windows()
	if dump {
		for i, s := range run.Said {
			fmt.Printf("=== %s %s\nbefore: %s\nmessage: %s\nafter: %s\nearlier: %s\n\n", s.Session, s.At.Local().Format(moment), oneLine(s.Before), s.Text, oneLine(s.After), states[i].Earlier)
		}
		for _, theme := range slices.Concat(run.Corrections, run.Watching, run.Seen) {
			fmt.Printf("theme %v places %d about agent %v present %d of %d\n", theme.Words, theme.Places, theme.AboutAgent, theme.Present(), len(theme.Checks))
			for _, q := range theme.Quotes {
				fmt.Printf("    %s %s %q\n", q.Session, q.At.Local().Format(moment), q.Text)
			}
		}
		return nil
	}
	hand, err := readLabels(file)
	if err != nil {
		return err
	}
	var windows []window
	for i, s := range run.Said {
		key := s.Session + " " + s.At.Local().Format(moment)
		if labelled, found := hand[key]; found {
			windows = append(windows, window{Key: key, Said: s, State: states[i], Hand: labelled})
		}
	}
	if len(windows) != len(hand) {
		return fmt.Errorf("%d hand labels and %d of them match a window of the chain", len(hand), len(windows))
	}
	fmt.Printf("bench learn · %s · %s · %d windows hand-labelled of %d\n\n", time.Now().Format(time.RFC3339), chain, len(windows), len(run.Said))
	jevLabels, jevCost, build, err := askJev(windows)
	if err != nil {
		return err
	}
	report("jev, one request per window, "+build, windows, jevLabels, jevCost)
	if !arm {
		return nil
	}
	armLabels, armCost, calls, err := askArm(windows)
	if err != nil {
		return err
	}
	report(fmt.Sprintf("arm, %s -p, one call per distilled transcript, %d calls", armCommand, calls), windows, armLabels, armCost)
	return nil
}

func readLabels(file string) (map[string]labels, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	hand := map[string]labels{}
	for n, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 6 {
			return nil, fmt.Errorf("%s:%d: session, time, corrects, bears_on, followed, lacks_info", file, n+1)
		}
		hand[fields[0]+" "+fields[1]] = labels{Nouls: map[string]bool{"corrects": fields[2] == yes, "bears_on": fields[3] == yes, "lacks_info": fields[5] == yes}, Followed: fields[4]}
	}
	return hand, nil
}

func askJev(windows []window) ([]labels, float64, string, error) {
	set, err := learn.Questions()
	if err != nil {
		return nil, 0, "", err
	}
	var questions []jev.Question
	for _, q := range set.Questions {
		questions = append(questions, q.ToJev())
	}
	key, err := jev.KeyFor(sys.CredentialFileName, jev.OpenRouterVariable)
	if err != nil {
		return nil, 0, "", err
	}
	wire, err := openrouter.New(openrouter.Config{Key: key, Transport: transport.Config{AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
		Retries: konst.JudgeRetries, Backoff: time.Duration(konst.JudgeBackoffMillis) * time.Millisecond, Concurrency: 1}})
	if err != nil {
		return nil, 0, "", err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return nil, 0, "", err
	}
	got := make([]labels, len(windows))
	cost, build := 0.0, ""
	for i, w := range windows {
		decision, err := client.Ask(context.Background(), jev.Request{State: w.State, Questions: questions})
		if err != nil {
			return nil, 0, "", fmt.Errorf("window %s: %w", w.Key, err)
		}
		got[i] = labels{Nouls: map[string]bool{}, Followed: decision.Answers[followedName].Choice}
		for _, name := range nouls {
			got[i].Nouls[name] = decision.Answers[name].Noul > argmaxOfTwo
		}
		cost, build = cost+decision.Usage.Cost, decision.Build
	}
	return got, cost, build, nil
}

func askArm(windows []window) ([]labels, float64, int, error) {
	set, err := learn.Questions()
	if err != nil {
		return nil, 0, 0, err
	}
	var asked strings.Builder
	for _, q := range set.Questions {
		fmt.Fprintf(&asked, "- %s: %s\n", q.Name, oneLine(q.Instructions))
	}
	got := make([]labels, len(windows))
	cost, calls := 0.0, 0
	var sessions []string
	for _, w := range windows {
		if !slices.Contains(sessions, w.Said.Session) {
			sessions = append(sessions, w.Said.Session)
		}
	}
	for _, name := range sessions {
		var transcript strings.Builder
		for i, w := range windows {
			if w.Said.Session == name {
				fmt.Fprintf(&transcript, "W%d\nbefore: %s\nmessage: %s\nafter: %s\nearlier: %s\n\n", i, oneLine(w.State.Before), w.State.Message, oneLine(w.State.After), w.State.Earlier)
			}
		}
		prompt := "Below is a distilled transcript of one coding-agent session: each window is the end of what the agent said (before), one message the person typed (message), what the agent said next (after), and an earlier instruction from the person in another session (earlier, maybe empty).\n\nFor every window answer these questions:\n" + asked.String() +
			"\nfollowed is one of followed, violated, irrelevant; the others are true or false. Reply with only a JSON object keyed by window id, for example {\"W3\": {\"corrects\": true, \"bears_on\": false, \"followed\": \"irrelevant\", \"lacks_info\": false}}.\n\n" + transcript.String()
		answer, spent, err := callArm(prompt)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("session %s: %w", name, err)
		}
		cost, calls = cost+spent, calls+1
		for i, w := range windows {
			if w.Said.Session != name {
				continue
			}
			one := answer[fmt.Sprintf("W%d", i)]
			got[i] = labels{Nouls: map[string]bool{}, Followed: fmt.Sprint(one[followedName])}
			for _, q := range nouls {
				got[i].Nouls[q] = one[q] == true
			}
		}
	}
	return got, cost, calls, nil
}

func callArm(prompt string) (map[string]map[string]any, float64, error) {
	command := exec.Command(armCommand, "-p", "--output-format", "json")
	command.Stdin, command.Dir = strings.NewReader(prompt), os.TempDir()
	var out, errOut bytes.Buffer
	command.Stdout, command.Stderr = &out, &errOut
	if err := command.Run(); err != nil {
		return nil, 0, fmt.Errorf("%w: %s", err, errOut.String())
	}
	var reply struct {
		Result string  `json:"result"`
		Cost   float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		return nil, 0, err
	}
	start, end := strings.Index(reply.Result, "{"), strings.LastIndex(reply.Result, "}")
	if start < 0 || end < start {
		return nil, reply.Cost, errors.New("the reply holds no JSON object: " + reply.Result)
	}
	var answer map[string]map[string]any
	return answer, reply.Cost, json.Unmarshal([]byte(reply.Result[start:end+1]), &answer)
}

func report(arm string, windows []window, got []labels, cost float64) {
	fmt.Printf("%s\n  cost $%.6f\n", arm, cost)
	for _, q := range append(slices.Clone(nouls), followedName) {
		agree := 0
		for i, w := range windows {
			if (q == followedName && got[i].Followed == w.Hand.Followed) || (q != followedName && got[i].Nouls[q] == w.Hand.Nouls[q]) {
				agree++
			}
		}
		fmt.Printf("  %-10s agrees with the hand label on %d of %d\n", q, agree, len(windows))
	}
	fmt.Println()
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
