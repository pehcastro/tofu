package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	benchapi "tofu/bench/api"
	"tofu/bench/corpus"
	"tofu/bench/report"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/judge/state"
	libraryquestions "tofu/library/questions"
)

const (
	shippedGateFile    = "library/general/rules/tool_gate@3.yaml"
	shippedQuestionSet = "tool_gate@3"
	hazardRuleFile     = "testdata/tool_gate_hazards.rule.json"
	armRegex           = "regex"
	armShipped         = "tool_gate@3"
	armHazards         = "tool_gate_hazards@1"
	hazardSpendCeiling = 0.05
)

type hazardRule struct {
	Questions            string  `json:"questions"`
	HazardAskAt          float64 `json:"hazard_ask_at"`
	RelaxedBy            string  `json:"relaxed_by"`
	UserRequestedRelaxAt float64 `json:"user_requested_relax_at"`
	NeverRelaxedBy       string  `json:"never_relaxed_by"`
}

func loadHazards() (hazardRule, question.Set, error) {
	data, err := os.ReadFile(hazardRuleFile)
	if err != nil {
		return hazardRule{}, question.Set{}, err
	}
	var rule hazardRule
	if err := json.Unmarshal(data, &rule); err != nil {
		return hazardRule{}, question.Set{}, fmt.Errorf("%s: %w", hazardRuleFile, err)
	}
	set, err := question.Load(rule.Questions)
	if err != nil {
		return hazardRule{}, question.Set{}, err
	}
	if findings := question.Lint(set, question.DefaultCaps()); len(findings) > 0 {
		return hazardRule{}, question.Set{}, fmt.Errorf("%s fails its own lint: %v", rule.Questions, findings[0])
	}
	for _, name := range []string{rule.RelaxedBy, rule.NeverRelaxedBy} {
		if _, ok := set.Question(name); !ok {
			return hazardRule{}, question.Set{}, fmt.Errorf("%s names %q and %s does not ask it", hazardRuleFile, name, rule.Questions)
		}
	}
	return rule, set, nil
}

func (r hazardRule) decide(answers map[string]jev.Answer) (Verdict, error) {
	if answers[r.NeverRelaxedBy].Noul >= r.HazardAskAt {
		return Block, nil
	}
	for name, answer := range answers {
		if name != r.RelaxedBy && answer.Noul >= r.HazardAskAt && answers[r.RelaxedBy].Noul <= r.UserRequestedRelaxAt {
			return Block, nil
		}
	}
	return Proceed, nil
}

func shippedQuestions() (question.Set, error) {
	layers, err := question.Layers(libraryquestions.Files(), "")
	if err != nil {
		return question.Set{}, err
	}
	set, _, err := question.Resolve(shippedQuestionSet, layers)
	return set, err
}

type jevArm struct {
	name      string
	rule      string
	set       question.Set
	questions []jev.Question
	decide    func(map[string]jev.Answer) (Verdict, error)
}

func armOf(name, rule string, set question.Set, decide func(map[string]jev.Answer) (Verdict, error)) jevArm {
	arm := jevArm{name: name, rule: rule, set: set, decide: decide}
	for _, q := range set.Questions {
		arm.questions = append(arm.questions, q.ToJev())
	}
	return arm
}

func jevArms(root string) ([]jevArm, error) {
	pol, _, err := gateRule(root, shippedGateFile)
	if err != nil {
		return nil, err
	}
	shipped, err := shippedQuestions()
	if err != nil {
		return nil, err
	}
	rule, hazards, err := loadHazards()
	if err != nil {
		return nil, err
	}
	decideShipped := func(answers map[string]jev.Answer) (Verdict, error) {
		verdict, _, err := decide(answers, pol)
		return verdict, err
	}
	return []jevArm{
		armOf(armShipped, "gate.Decide over "+shippedGateFile, shipped, decideShipped),
		armOf(armHazards, "any hazard noul at or over its threshold blocks, over bench/cost/"+hazardRuleFile, hazards, rule.decide),
	}, nil
}

func heldOutRecords() ([]corpus.Record, error) {
	records, err := corpus.GateRecords()
	if err != nil {
		return nil, err
	}
	split, err := corpus.GateSplit()
	if err != nil {
		return nil, err
	}
	byID := make(map[string]corpus.Record, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}
	heldOut := make([]corpus.Record, 0, len(split.Heldout))
	for _, id := range split.Heldout {
		record, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("bench/cost: the split names %s and the corpus does not hold it", id)
		}
		heldOut = append(heldOut, record)
	}
	return heldOut, nil
}

func gateStateOf(record corpus.Record) (json.RawMessage, error) {
	raw, err := json.Marshal(record.State)
	if err != nil {
		return nil, err
	}
	var call struct {
		Agent   string                `json:"agent"`
		Tool    string                `json:"tool"`
		Input   map[string]any        `json:"input"`
		Cwd     string                `json:"cwd"`
		Context state.ToolGateContext `json:"context"`
	}
	if err := json.Unmarshal(raw, &call); err != nil {
		return nil, fmt.Errorf("case %s: %w", record.ID, err)
	}
	built, _, err := state.BuildToolGateV3(state.ToolGateInput{
		Agent: call.Agent, Tool: call.Tool, Input: call.Input,
		Cwd: call.Cwd, ProjectDir: call.Cwd, Context: call.Context,
	})
	return built, err
}

type hazardRow struct {
	Case           string             `json:"case"`
	Arm            string             `json:"arm"`
	Label          Verdict            `json:"label"`
	Verdict        Verdict            `json:"verdict"`
	Build          string             `json:"build"`
	Wire           string             `json:"wire"`
	CredentialKind string             `json:"credential_kind"`
	Machine        string             `json:"machine"`
	Date           string             `json:"date"`
	InputTokens    int                `json:"input_tokens"`
	OutputTokens   int                `json:"output_tokens"`
	ProviderUSD    float64            `json:"provider_usd"`
	LatencyMS      float64            `json:"latency_ms"`
	Attempts       int                `json:"attempts"`
	Answers        map[string]float64 `json:"answers"`
}

func (r hazardRow) charge() ledger.Charge {
	return ledger.ChargeOf(r.ProviderUSD, float64(r.InputTokens)*typesafe.DollarsPerInputToken)
}

func answersOf(set question.Set, values map[string]float64) (map[string]jev.Answer, error) {
	answers := make(map[string]jev.Answer, len(set.Questions))
	for _, q := range set.Questions {
		value, ok := values[q.Name]
		if !ok {
			return nil, fmt.Errorf("the row carries no %s answer", q.Name)
		}
		switch q.Kind {
		case question.KindScore:
			answers[q.Name] = jev.Answer{Kind: jev.QuestionScore, Score: value}
		case question.KindNoul:
			answers[q.Name] = jev.Answer{Kind: jev.QuestionNoul, Noul: value}
		default:
			return nil, fmt.Errorf("%s is a %s and the bench reads only scores and nouls", q.Name, q.Kind)
		}
	}
	return answers, nil
}

func runHazardArms(ctx context.Context, key string, arms []jevArm, records []corpus.Record, conditions report.Conditions) ([]hazardRow, error) {
	wire, err := benchapi.NewWire(key)
	if err != nil {
		return nil, err
	}
	var rows []hazardRow
	spent := 0.0
	for i, record := range records {
		built, err := gateStateOf(record)
		if err != nil {
			return rows, err
		}
		order := arms
		if i%2 == 1 {
			order = []jevArm{arms[1], arms[0]}
		}
		for _, arm := range order {
			call := benchapi.Ask(ctx, wire, jev.Request{State: built, Questions: arm.questions})
			if call.Err != nil {
				return rows, fmt.Errorf("case %s, arm %s: %w", record.ID, arm.name, call.Err)
			}
			verdict, err := arm.decide(call.Response.Answers)
			if err != nil {
				return rows, fmt.Errorf("case %s, arm %s: %w", record.ID, arm.name, err)
			}
			row := hazardRow{
				Case: record.ID, Arm: arm.name, Label: verdictOf(record.Label), Verdict: verdict,
				Build: call.Response.Build, Wire: conditions.Wire, CredentialKind: conditions.CredentialKind,
				Machine: conditions.Machine, Date: conditions.Date,
				InputTokens: call.Response.Usage.InputTokens, OutputTokens: call.Response.Usage.OutputTokens,
				ProviderUSD: call.Response.Usage.Cost, LatencyMS: msSince(call.Raw.Latency), Attempts: call.Raw.Attempts,
				Answers: map[string]float64{},
			}
			for _, q := range arm.set.Questions {
				answer := call.Response.Answers[q.Name]
				row.Answers[q.Name] = answer.Noul
				if q.Kind == question.KindScore {
					row.Answers[q.Name] = answer.Score
				}
			}
			rows = append(rows, row)
			if spent += row.charge().USD; spent > hazardSpendCeiling {
				return rows, fmt.Errorf("stopped at case %s: $%.6f spent, over the $%.2f ceiling", record.ID, spent, hazardSpendCeiling)
			}
		}
	}
	return rows, nil
}

type hazardScore struct {
	Arm         string
	Rule        string
	Cases       int
	Blocks      int
	Correct     int
	FalseBlocks int
	Caught      int
	USD         []float64
	LatencyMS   []float64
	InputTokens []float64
	Builds      map[string]int
	Charges     map[ledger.ChargeSource]int
}

func (s *hazardScore) tally(verdict, label Verdict) {
	s.Cases++
	if label == Block {
		s.Blocks++
	}
	if verdict == label {
		s.Correct++
	}
	if verdict == Block && label == Block {
		s.Caught++
	}
	if verdict == Block && label == Proceed {
		s.FalseBlocks++
	}
}

func scoreHazards(rows []hazardRow, arms []jevArm, records []corpus.Record) ([]hazardScore, error) {
	regex := hazardScore{Arm: armRegex, Rule: "cost.RegexDecide over the recorded command and the first user message"}
	for _, record := range records {
		regex.tally(RegexDecide(probeOf(record.State)), verdictOf(record.Label))
	}
	scores := []hazardScore{regex}
	for _, arm := range arms {
		score := hazardScore{Arm: arm.name, Rule: arm.rule, Builds: map[string]int{}, Charges: map[ledger.ChargeSource]int{}}
		byCase := map[string]hazardRow{}
		for _, row := range rows {
			if row.Arm != arm.name {
				continue
			}
			if _, twice := byCase[row.Case]; twice {
				return nil, fmt.Errorf("arm %s answers case %s twice", arm.name, row.Case)
			}
			byCase[row.Case] = row
		}
		for _, record := range records {
			row, ok := byCase[record.ID]
			if !ok {
				return nil, fmt.Errorf("arm %s has no answer for case %s", arm.name, record.ID)
			}
			answers, err := answersOf(arm.set, row.Answers)
			if err != nil {
				return nil, fmt.Errorf("arm %s, case %s: %w", arm.name, record.ID, err)
			}
			verdict, err := arm.decide(answers)
			if err != nil {
				return nil, fmt.Errorf("arm %s, case %s: %w", arm.name, record.ID, err)
			}
			if verdict != row.Verdict || row.Label != verdictOf(record.Label) {
				return nil, fmt.Errorf("arm %s, case %s: recorded %s against %s, and %s now decides %s against %s", arm.name, record.ID, row.Verdict, row.Label, arm.rule, verdict, verdictOf(record.Label))
			}
			score.tally(verdict, row.Label)
			charge := row.charge()
			score.USD = append(score.USD, charge.USD)
			score.Charges[charge.Source]++
			score.LatencyMS = append(score.LatencyMS, row.LatencyMS)
			score.InputTokens = append(score.InputTokens, float64(row.InputTokens))
			score.Builds[row.Build]++
		}
		scores = append(scores, score)
	}
	return scores, nil
}

func readHazardRows(path string) ([]hazardRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []hazardRow
	for index, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row hazardRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, index+1, err)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func writeHazardRows(path string, rows []hazardRow) error {
	body := &bytes.Buffer{}
	for _, row := range rows {
		line, err := json.Marshal(row)
		if err != nil {
			return err
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	return report.Write(path, body.Bytes(), 0o644, "hazard answer file")
}

func hazardPaths(day string) (answers, rendered string) {
	return filepath.Join("answers", "hazard-"+day+".jsonl"), "hazard-" + day + ".md"
}
