package stopcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"tofu/internal/judge/gate"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/sys"
	"tofu/internal/transport"
	libraryquestions "tofu/library/questions"
)

const Point = state.StopCheckRuleRef

type StepResult struct {
	Turn      string
	Step      int
	Label     Label
	Cheap     Cheap
	Verdict   ledger.Verdict
	Typed     Answer
	Build     string
	Answers   map[string]float64
	RowID     string
	Replayed  bool
	LatencyMS int64
	Cost      float64
}

type TurnCost struct {
	Turn          string
	Steps         int
	GateDecisions int
	GateMissing   int
	GateLatencyMS int64
	GateCost      float64
	StopDecisions int
	StopLatencyMS int64
	StopCost      float64
	StopFresh     int
}

type Result struct {
	GeneratedAt       time.Time
	Build             string
	Wording           int
	Mode              gate.Mode
	ModeReason        string
	Steps             []StepResult
	Turns             []TurnCost
	Skipped           []Skipped
	TurnsWithoutSteps []string
	Unlabelled        []string
}

type Battery struct {
	client   *jev.Client
	battery  []jev.Question
	wording  int
	pol      gate.Rule
	mode     gate.Mode
	reason   string
	cache    *ledger.Cache
	writer   *ledger.Writer
	recorded *ledger.Reader
}

func New(root, key string) (Battery, error) {
	layers, err := question.DefaultLayers(libraryquestions.Files())
	if err != nil {
		return Battery{}, err
	}
	set, _, err := question.Resolve(Point, layers)
	if err != nil {
		return Battery{}, err
	}
	pol, _, err := state.StopCheckRule()
	if err != nil {
		return Battery{}, err
	}
	if pol.Questions != set.Name || pol.QuestionsVersion != set.QuestionsVersion {
		return Battery{}, fmt.Errorf("rule %s names question set %s@%d and the loaded setis %s@%d",
			Point, pol.Questions, pol.QuestionsVersion, set.Name, set.QuestionsVersion)
	}
	resolution := gate.Resolve(pol, gate.LockLookup{}, gate.Current{})
	pol = resolution.Rule

	wire, err := openrouter.New(openrouter.Config{
		Key: key,
		Transport: transport.Config{
			AttemptTimeout: time.Duration(konst.JudgeTimeoutMillis) * time.Millisecond,
			Retries:        konst.JudgeRetries,
			Backoff:        time.Duration(konst.JudgeBackoffMillis) * time.Millisecond,
			Concurrency:    1,
		},
	})
	if err != nil {
		return Battery{}, err
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		return Battery{}, err
	}
	stateDir := sys.StateDir(root)
	cacheDir := filepath.Join(stateDir, "cache")
	logDir := filepath.Join(stateDir, "log")
	questions := make([]jev.Question, 0, len(set.Questions))
	for _, q := range set.Questions {
		questions = append(questions, q.ToJev())
	}
	return Battery{
		client:   client,
		battery:  questions,
		wording:  set.QuestionsVersion,
		pol:      pol,
		mode:     resolution.Mode,
		reason:   resolution.Reason,
		cache:    ledger.NewCache(cacheDir),
		writer:   ledger.NewWriter(logDir),
		recorded: ledger.NewReader(logDir),
	}, nil
}

func (b Battery) Run(ctx context.Context, turns []Turn, skipped []Skipped) (Result, error) {
	result := Result{GeneratedAt: time.Now(), Wording: b.wording, Mode: b.mode, ModeReason: b.reason, Skipped: skipped}
	labels := Labels()
	for _, turn := range turns {
		if len(turn.Steps) == 0 {
			result.TurnsWithoutSteps = append(result.TurnsWithoutSteps, turn.ID)
			continue
		}
		cost := TurnCost{Turn: turn.ID, Steps: len(turn.Steps)}
		for at, step := range turn.Steps {
			key := stepKey(turn.ID, step.Index)
			label, labelled := labels[key]
			if !labelled {
				result.Unlabelled = append(result.Unlabelled, key)
				continue
			}
			decided, err := b.decide(ctx, turn, at)
			if err != nil {
				return Result{}, fmt.Errorf("%s: %w", key, err)
			}
			decided.Label, decided.Cheap = label, CheapArm(turn, at)
			result.Steps = append(result.Steps, decided)
			if result.Build == "" {
				result.Build = decided.Build
			}
			cost.StopCost += decided.Cost
			cost.StopLatencyMS += decided.LatencyMS
			cost.StopDecisions++
			if !decided.Replayed {
				cost.StopFresh++
			}
		}
		if err := b.addGateSpend(turn, &cost); err != nil {
			return Result{}, err
		}
		result.Turns = append(result.Turns, cost)
	}
	return result, nil
}

func (b Battery) decide(ctx context.Context, turn Turn, at int) (StepResult, error) {
	built, builder, err := state.BuildStopCheck(StateAt(turn, at))
	if err != nil {
		return StepResult{}, err
	}
	raw := json.RawMessage(built)
	request := ledger.Request{State: raw, Questions: b.pol.Questions, Model: openrouter.Alias, Version: b.wording}
	key, err := b.cache.Key(request)
	if err != nil {
		return StepResult{}, err
	}
	entry, hit, err := b.cache.Load(key)
	if err != nil {
		return StepResult{}, err
	}

	in := rowInput{
		state: raw, stateBuilder: builder, wording: b.wording,
		pol: b.pol, mode: b.mode, modeReason: b.reason, turnID: turn.ID,
	}
	out := StepResult{Turn: turn.ID, Step: turn.Steps[at].Index, Replayed: hit}
	if hit {
		in.answers, in.build, in.requestID, in.replayOf = entry.Answers, entry.Build, entry.RequestID, entry.RowID
		measured, present, err := b.recorded.ByID(entry.RowID)
		if err != nil {
			return StepResult{}, err
		}
		if present {
			out.LatencyMS, out.Cost = measured.LatencyMS, measured.Cost
		}
	} else {
		decision, err := b.client.Ask(ctx, jev.Request{State: raw, Questions: b.battery})
		if err != nil {
			return StepResult{}, err
		}
		in.answers = toLedgerAnswers(b.wording, decision.Answers)
		in.build, in.requestID = decision.Build, decision.RequestID
		in.latencyMS, in.cost = decision.Latency.Milliseconds(), decision.Usage.Cost
		out.LatencyMS, out.Cost = in.latencyMS, in.cost
	}

	row, err := appendRow(b.writer, in)
	if err != nil {
		return StepResult{}, err
	}
	if !hit {
		stored := ledger.Entry{RowID: row.ID, Build: row.Build, RequestID: row.RequestID, Answers: in.answers}
		if err := b.cache.Store(key, request, stored); err != nil {
			return StepResult{}, err
		}
	}
	out.RowID, out.Verdict, out.Build = row.ID, row.Verdict, row.Build
	out.Typed = typedAnswer(row.Verdict)
	out.Answers = answerValues(in.answers)
	return out, nil
}

func typedAnswer(verdict ledger.Verdict) Answer {
	if verdict == ledger.VerdictAllow {
		return Continue
	}
	return Stop
}

func answerValues(answers []ledger.Answer) map[string]float64 {
	out := make(map[string]float64, len(answers))
	for _, a := range answers {
		switch a.Kind {
		case ledger.AnswerNoul:
			out[a.Question] = a.Noul
		case ledger.AnswerScore:
			out[a.Question] = a.Score
		case ledger.AnswerChoice:
			panic("stopcheck: stop_check asks no choice question and " + a.Question + " came back as one")
		}
	}
	return out
}

func (b Battery) addGateSpend(turn Turn, cost *TurnCost) error {
	for _, step := range turn.Steps {
		for _, call := range step.Calls {
			if call.GateDecision == "" {
				cost.GateMissing++
				continue
			}
			row, present, err := b.recorded.ByID(call.GateDecision)
			if err != nil {
				return err
			}
			if !present {
				cost.GateMissing++
				continue
			}
			cost.GateDecisions++
			cost.GateLatencyMS += row.LatencyMS
			cost.GateCost += row.Cost
		}
	}
	return nil
}

func stepKey(turn string, index int) string {
	return turn + "#" + strconv.Itoa(index)
}
