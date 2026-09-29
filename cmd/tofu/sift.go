package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"tofu/interface/cli"
	"tofu/internal/judge/jev"
	"tofu/internal/konst"
	"tofu/internal/sift"
)

const siftPoint = "read_worth@1"

const (
	armLength   = "length"
	armSignpost = "signpost"
	armBrevity  = "brevity"
	armJev      = "jev"
)

var siftUsage = fmt.Sprintf(`tofu sift marks each paragraph of standard input as kept or elided.

Arms:
  length    default. drop a paragraph under a %d-word floor, keep the rest.
            no live call. bench/readworth/report-2026-09-21.md measured this
            arm at 87.1%% accuracy and more bytes saved than jev, on both
            live runs against the same corpus.
  signpost  drop a markdown heading, or a short line ending in a colon.
            no live call.
  brevity   flag banned words, a scorecard, a run of bold, an opening that
            agrees. no live call.
  jev       ask read_worth@1 to score answers_the_task against the padding
            questions. one live call per paragraph, with cost and latency.
            it lost the readworth bench on accuracy at equal savings; kept
            reachable because a corpus is not a proof for all time.

Flags:
  --arm NAME    length (default), signpost, brevity or jev
  --task TEXT   the reader's request, read by the jev arm only
  --restore     undo a previous sift, reading the fence back to the original
  --no-log      with --arm jev, skip the decision ledger
  --json        one envelope on stdout carrying the text and the counts
`, konst.SiftReadWorthWordFloor)

const siftUsageLine = "tofu sift [--arm length|signpost|brevity|jev] [--task text] [--restore] [--no-log] [--json] < text"

type siftOpts struct {
	task    string
	arm     string
	restore bool
	noLog   bool
	asJSON  bool
}

type siftReport struct {
	Arm        string  `json:"arm"`
	Paragraphs int     `json:"paragraphs"`
	Kept       int     `json:"kept"`
	Words      int     `json:"words"`
	KeptWords  int     `json:"kept_words"`
	ElapsedMS  int64   `json:"elapsed_ms"`
	Cost       float64 `json:"cost_usd"`
	Text       string  `json:"text"`
}

func siftVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		_, _ = io.WriteString(out, siftUsage)
		return exitOK
	}
	o := verbOutput{verb: "sift", usageLine: siftUsageLine, out: out, errOut: errOut}
	opts, err := parseSiftArgs(args)
	if err != nil {
		return o.usage(err)
	}
	o.asJSON = opts.asJSON
	raw, err := io.ReadAll(in)
	if err != nil {
		return failed(o, fmt.Errorf("reading standard input: %w", err))
	}
	text := string(raw)

	if opts.restore {
		original, err := sift.Restore(text)
		if err != nil {
			return failed(o, err)
		}
		return protocol(o, exitOK, struct {
			Text string `json:"text"`
		}{original}, original)
	}

	parts, err := sift.Split(text)
	if err != nil {
		return failed(o, err)
	}
	if len(parts) == 0 {
		return failed(o, errors.New("standard input carries no text"))
	}

	started := time.Now()
	marks, cost, err := siftMarks(parts, opts)
	if err != nil {
		return failed(o, err)
	}
	report := siftReport{Arm: opts.arm, Paragraphs: len(parts), ElapsedMS: time.Since(started).Milliseconds(), Cost: cost, Text: sift.Render(parts, marks)}
	for i, p := range parts {
		report.Words += p.Words()
		if marks[i].Keep {
			report.Kept++
			report.KeptWords += p.Words()
		}
	}
	if !opts.asJSON {
		page := cli.Detect(errOut, os.Environ())
		_ = page.Print(errOut, []string{page.Label(report.Arm) + cli.Gap + fmt.Sprintf("%d/%d paragraphs kept · %d/%d words · %dms · $%.6f",
			report.Kept, report.Paragraphs, report.KeptWords, report.Words, report.ElapsedMS, report.Cost)})
	}
	return protocol(o, exitOK, report, report.Text)
}

func siftMarks(parts []sift.Part, opts siftOpts) ([]sift.Mark, float64, error) {
	if opts.arm == armJev {
		return siftJev(parts, opts)
	}
	judge := sift.Length
	switch opts.arm {
	case armSignpost:
		judge = sift.Signpost
	case armBrevity:
		judge = sift.Cheap
	}
	marks := make([]sift.Mark, len(parts))
	for i, p := range parts {
		marks[i] = judge(p)
	}
	return marks, 0, nil
}

func siftJev(parts []sift.Part, opts siftOpts) ([]sift.Mark, float64, error) {
	marks := make([]sift.Mark, len(parts))
	set, err := resolveLibrary(siftPoint, "")
	if err != nil {
		return nil, 0, err
	}
	key, err := gateKey()
	if err != nil {
		return nil, 0, err
	}
	client, err := jevClientOn(key, konst.SiftConcurrency)
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

func parseSiftArgs(args []string) (siftOpts, error) {
	opts := siftOpts{arm: armLength}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--arm":
			i++
			if i >= len(args) {
				return siftOpts{}, fmt.Errorf("--arm needs one of %s, %s, %s or %s", armLength, armSignpost, armBrevity, armJev)
			}
			opts.arm = args[i]
			if opts.arm != armLength && opts.arm != armSignpost && opts.arm != armBrevity && opts.arm != armJev {
				return siftOpts{}, fmt.Errorf("%q is not an arm; the arms are %s, %s, %s and %s", opts.arm, armLength, armSignpost, armBrevity, armJev)
			}
		case "--restore":
			opts.restore = true
		case "--no-log":
			opts.noLog = true
		case jsonFlag:
			opts.asJSON = true
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
