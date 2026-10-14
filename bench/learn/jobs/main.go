package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/learn"
	"tofu/internal/session"
	"tofu/internal/sys"
)

type step struct {
	Agent  string   `json:"agent"`
	Tools  []string `json:"tools"`
	Tokens int      `json:"tokens"`
	Cost   float64  `json:"cost"`
}

type turn struct {
	Minute  int    `json:"minute"`
	Origin  string `json:"origin"`
	Wrapped bool   `json:"wrapped"`
	Job     string `json:"job"`
	Said    string `json:"said"`
	Steps   []step `json:"steps"`
}

type corpus struct {
	Start    time.Time `json:"start"`
	Wrapper  string    `json:"wrapper"`
	Sessions []struct {
		Name  string `json:"name"`
		Day   int    `json:"day"`
		Turns []turn `json:"turns"`
	} `json:"sessions"`
}

func main() {
	file := flag.String("corpus", "bench/learn/jobs/corpus.json", "the mocked sessions and their hand labels")
	seed := flag.String("seed", "", "write the corpus as the sessions of this project under the current home, instead of a scratch store")
	flag.Parse()
	if err := bench(*file, *seed); err != nil {
		fmt.Fprintln(os.Stderr, "bench learn jobs:", err)
		os.Exit(1)
	}
}

func labelOf(name string, at time.Time) string {
	return name + " " + at.UTC().Format(time.RFC3339)
}

func write(store *session.Store, c corpus) (map[string]string, error) {
	labels := map[string]string{}
	for _, s := range c.Sessions {
		name := s.Name
		day := c.Start.Add(time.Duration(s.Day) * 24 * time.Hour)
		log, err := store.Open(session.Header{Name: &name, At: day})
		if err != nil {
			return nil, err
		}
		add := func(event session.Event, body any) {
			if err == nil {
				_, err = log.Append(event, body)
			}
		}
		for _, t := range s.Turns {
			at := day.Add(time.Duration(t.Minute) * time.Minute)
			text := t.Said
			if t.Wrapped {
				text = c.Wrapper + text
			}
			add(session.Event{Kind: session.EventMessage, At: at}, session.MessageBody{Role: session.RoleUser, Content: text, Origin: t.Origin, TakenAt: &at})
			if t.Job != "" {
				labels[labelOf(s.Name, at)] = t.Job
			}
			for k, st := range t.Steps {
				stepAt := at.Add(time.Duration(k+1) * time.Minute)
				add(session.Event{Kind: session.EventRequest, At: stepAt, Agent: st.Agent}, session.StepBody{PromptTokens: st.Tokens, CostUSD: st.Cost})
				for _, tool := range st.Tools {
					add(session.Event{Kind: session.EventToolCall, At: stepAt.Add(time.Second), Agent: st.Agent}, session.CallBody{Tool: tool, Args: json.RawMessage(`{}`)})
				}
			}
		}
		if err := errors.Join(err, log.Close()); err != nil {
			return nil, err
		}
	}
	return labels, nil
}

func bench(file, seed string) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "tofu-bench-learn-jobs")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	store := session.NewStore(scratch)
	if seed != "" {
		if store, err = session.OpenIn(seed); err != nil {
			return err
		}
	}
	labels, err := write(store, c)
	if err != nil {
		return err
	}
	headers, err := learn.Recent(store, len(c.Sessions))
	if err != nil {
		return err
	}
	run, err := learn.Scan([]learn.Source{{Store: store, Headers: headers}}, learn.Known{}, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("bench learn jobs · tofu %s commit %s · %s · %d mocked sessions · %s\n\n", konst.Version, sys.BuildRevision(), file, len(c.Sessions), time.Now().Format(time.RFC3339))
	found, falses := map[string]bool{}, 0
	for _, job := range run.Jobs {
		var named []string
		for _, r := range job.Runs {
			named = append(named, labels[labelOf(r.Session, r.At)])
		}
		label := named[0]
		if label == "" || slices.ContainsFunc(named, func(n string) bool { return n != label }) {
			falses++
			label = "false: " + strings.Join(named, ",")
		}
		found[label] = true
		fmt.Printf("  %-22s %d times · %.4f USD each · %.4f USD total · %s\n", label, job.Times, job.CostEach, job.Cost, strings.Join(job.Tools, " > "))
	}
	counted := map[string]int{}
	for _, job := range labels {
		counted[job]++
	}
	labelled, hit := 0, 0
	for job, times := range counted {
		if times < learn.RepeatedJobTimes {
			continue
		}
		labelled++
		if found[job] {
			hit++
			continue
		}
		fmt.Printf("  %-22s missed, %d labelled runs\n", job, times)
	}
	fmt.Printf("\nlabelled %d · found %d · missed %d · false %d\n", labelled, hit, labelled-hit, falses)
	return nil
}
