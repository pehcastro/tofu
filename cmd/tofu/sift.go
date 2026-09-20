package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"tofu/internal/judge/jev"
	jevwire "tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/konst"
	"tofu/internal/sift"
	"tofu/internal/transport"
)

const siftPoint = "read_worth@1"

const (
	armSignpost = "signpost"
	armBrevity  = "brevity"
	armJev      = "jev"
)

type siftOpts struct {
	task    string
	arm     string
	restore bool
	noLog   bool
}

func siftVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	opts, err := parseSiftArgs(args)
	if err != nil {
		return siftFail(errOut, err)
	}
	raw, err := io.ReadAll(in)
	if err != nil {
		return siftFail(errOut, fmt.Errorf("reading standard input: %w", err))
	}
	text := string(raw)

	if opts.restore {
		original, err := sift.Restore(text)
		if err != nil {
			return siftFail(errOut, err)
		}
		_, _ = io.WriteString(out, original)
		return exitOK
	}

	parts, err := sift.Split(text)
	if err != nil {
		return siftFail(errOut, err)
	}
	if len(parts) == 0 {
		return siftFail(errOut, errors.New("standard input carries no text"))
	}

	started := time.Now()
	marks, cost, err := siftMarks(parts, opts)
	if err != nil {
		return siftFail(errOut, err)
	}
	_, _ = io.WriteString(out, sift.Render(parts, marks))

	kept, keptWords, words := 0, 0, 0
	for i, p := range parts {
		words += p.Words()
		if marks[i].Keep {
			kept++
			keptWords += p.Words()
		}
	}
	_, _ = fmt.Fprintf(errOut, "%s: kept %d/%d paragraphs, %d/%d words, %s, $%.6f\n",
		opts.arm, kept, len(parts), keptWords, words, time.Since(started).Round(time.Millisecond), cost)
	return exitOK
}

func siftMarks(parts []sift.Part, opts siftOpts) ([]sift.Mark, float64, error) {
	marks := make([]sift.Mark, len(parts))
	if opts.arm != armJev {
		judge := sift.Signpost
		if opts.arm == armBrevity {
			judge = sift.Cheap
		}
		for i, p := range parts {
			marks[i] = judge(p)
		}
		return marks, 0, nil
	}

	set, err := resolveCatalog(siftPoint)
	if err != nil {
		return nil, 0, err
	}
	key, err := gateKey()
	if err != nil {
		return nil, 0, err
	}
	wire, err := jevwire.New(jevwire.Config{
		Key: key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    konst.SiftConcurrency,
		},
	})
	if err != nil {
		return nil, 0, err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return nil, 0, err
	}

	rule := sift.DefaultRule()
	costs := make([]float64, len(parts))
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	for i := range parts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			mark, cost, err := siftOne(client, set, parts, i, opts, rule)
			marks[i], costs[i], errs[i] = mark, cost, err
		}(i)
	}
	wg.Wait()

	total := 0.0
	for i := range parts {
		total += costs[i]
		if errs[i] != nil {
			return nil, 0, errs[i]
		}
	}
	return marks, total, nil
}

func siftOne(client *jev.Client, set battery, parts []sift.Part, index int, opts siftOpts, rule sift.Rule) (sift.Mark, float64, error) {
	state, err := json.Marshal(sift.BuildState(parts, index, opts.task))
	if err != nil {
		return sift.Mark{}, 0, err
	}
	built := json.RawMessage(state)
	decision, err := client.Ask(context.Background(), jev.Request{State: built, Questions: set.Questions})
	if err != nil {
		return sift.Mark{Keep: true, Reason: "kept: " + err.Error()}, 0, nil
	}
	scores := make(map[string]float64, len(decision.Answers))
	for id, answer := range decision.Answers {
		scores[id] = answer.Score
	}
	mark, err := sift.Decide(scores, rule)
	if err != nil {
		return sift.Mark{}, decision.Usage.Cost, err
	}
	if !opts.noLog {
		if _, err := appendRow(built, set, rowInput{
			decision:     &decision,
			answers:      toLedgerAnswers(set.QuestionsVersion, decision.Answers),
			stateBuilder: siftPoint,
		}); err != nil {
			return sift.Mark{}, decision.Usage.Cost, err
		}
	}
	return mark, decision.Usage.Cost, nil
}

func siftFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu sift: %v\n", err)
	return exitUsage
}

func parseSiftArgs(args []string) (siftOpts, error) {
	opts := siftOpts{arm: armSignpost}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--arm":
			i++
			if i >= len(args) {
				return siftOpts{}, fmt.Errorf("--arm needs one of %s, %s or %s", armSignpost, armBrevity, armJev)
			}
			opts.arm = args[i]
			if opts.arm != armSignpost && opts.arm != armBrevity && opts.arm != armJev {
				return siftOpts{}, fmt.Errorf("%q is not an arm; the arms are %s, %s and %s", opts.arm, armSignpost, armBrevity, armJev)
			}
		case "--restore":
			opts.restore = true
		case "--no-log":
			opts.noLog = true
		case "--task":
			i++
			if i >= len(args) {
				return siftOpts{}, errors.New("--task needs the reader's request")
			}
			opts.task = args[i]
		default:
			return siftOpts{}, fmt.Errorf("unknown argument %q", args[i])
		}
	}
	return opts, nil
}
