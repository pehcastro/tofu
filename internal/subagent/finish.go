package subagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type Level int

const (
	Low Level = iota
	Medium
	High
	levelCount
)

func (l Level) String() string {
	switch l {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	}
	panic("subagent: unknown confidence level " + strconv.Itoa(int(l)))
}

func (Level) Enum() []string { return names(levelCount) }

func (l Level) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }

func (l *Level) UnmarshalJSON(data []byte) error {
	read, err := byName(data, levelCount, "confidence level")
	if err != nil {
		return err
	}
	*l = read
	return nil
}

type FileChange struct {
	Path   string `json:"path"`
	Change string `json:"change"`
	Seen   bool   `json:"seen"`
}

type Check struct {
	Command string `json:"command"`
	Exit    *int   `json:"exit"`
	Excerpt string `json:"excerpt,omitempty"`
	Seen    bool   `json:"seen"`
}

type Confidence struct {
	Level  Level  `json:"level"`
	Reason string `json:"reason"`
}

type OpenQuestion struct {
	Question string `json:"question"`
	SettleBy string `json:"settleBy"`
}

type Ask struct {
	Kind string `json:"kind"`
	Ask  string `json:"ask"`
}

type Measured struct {
	Finished bool    `json:"finished"`
	Outcome  string  `json:"outcome"`
	Steps    int     `json:"steps"`
	CostUSD  float64 `json:"costUsd"`
}

type Result struct {
	Verdict       Completion     `json:"verdict"`
	Summary       string         `json:"summary"`
	Files         []FileChange   `json:"files"`
	Checks        []Check        `json:"checks"`
	Claims        []Claim        `json:"claims,omitempty"`
	Findings      []Finding      `json:"findings"`
	Confidence    Confidence     `json:"confidence"`
	OpenQuestions []OpenQuestion `json:"openQuestions,omitempty"`
	Asks          []Ask          `json:"asks,omitempty"`
	Consulted     []string       `json:"consulted,omitempty"`
	Body          string         `json:"body,omitempty"`
	Measured      Measured       `json:"measured"`
}

func AskKinds() []string { return names(Grant) }

func ReadResult(raw json.RawMessage) (Result, error) {
	var result Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return Result{}, err
	}
	var missing []string
	need := func(empty bool, field string) {
		if empty {
			missing = append(missing, field)
		}
	}
	need(strings.TrimSpace(result.Summary) == "", "summary")
	need(strings.TrimSpace(result.Confidence.Reason) == "", "confidence.reason")
	for i, file := range result.Files {
		need(strings.TrimSpace(file.Path) == "", "files["+strconv.Itoa(i)+"].path")
		need(strings.TrimSpace(file.Change) == "", "files["+strconv.Itoa(i)+"].change")
	}
	for i, check := range result.Checks {
		need(strings.TrimSpace(check.Command) == "", "checks["+strconv.Itoa(i)+"].command")
		need(check.Exit == nil, "checks["+strconv.Itoa(i)+"].exit")
	}
	for i, claim := range result.Claims {
		need(strings.TrimSpace(claim.Line) == "", "claims["+strconv.Itoa(i)+"].line")
	}
	for i, finding := range result.Findings {
		need(strings.TrimSpace(finding.Text) == "", "findings["+strconv.Itoa(i)+"].text")
	}
	for i, open := range result.OpenQuestions {
		need(strings.TrimSpace(open.Question) == "", "openQuestions["+strconv.Itoa(i)+"].question")
		need(strings.TrimSpace(open.SettleBy) == "", "openQuestions["+strconv.Itoa(i)+"].settleBy")
	}
	for i, ask := range result.Asks {
		need(!slices.Contains(AskKinds(), ask.Kind), "asks["+strconv.Itoa(i)+"].kind as one of "+strings.Join(AskKinds(), ", "))
		need(strings.TrimSpace(ask.Ask) == "", "asks["+strconv.Itoa(i)+"].ask")
	}
	if len(missing) > 0 {
		return Result{}, errors.New("these fields are missing or empty: " + strings.Join(missing, ", "))
	}
	if result.Files == nil {
		result.Files = []FileChange{}
	}
	if result.Checks == nil {
		result.Checks = []Check{}
	}
	if result.Findings == nil {
		result.Findings = []Finding{}
	}
	return result, nil
}

func (r Result) Text() string {
	body := &strings.Builder{}
	fmt.Fprintf(body, "it finished %s: %s\n", r.Verdict, r.Summary)
	for _, file := range r.Files {
		fmt.Fprintf(body, "file %s: %s%s\n", file.Path, file.Change, unseen(file.Seen))
	}
	for _, check := range r.Checks {
		fmt.Fprintf(body, "check %s exited %d%s", check.Command, *check.Exit, unseen(check.Seen))
		if check.Excerpt != "" {
			fmt.Fprintf(body, ", printing %s", check.Excerpt)
		}
		body.WriteString("\n")
	}
	fmt.Fprintf(body, "confidence %s: %s\n", r.Confidence.Level, r.Confidence.Reason)
	for _, open := range r.OpenQuestions {
		fmt.Fprintf(body, "open question: %s, settled by %s\n", open.Question, open.SettleBy)
	}
	for _, ask := range r.Asks {
		fmt.Fprintf(body, "asks a %s: %s\n", ask.Kind, ask.Ask)
	}
	if len(r.Consulted) > 0 {
		fmt.Fprintf(body, "consulted %s\n", strings.Join(r.Consulted, ", "))
	}
	return body.String()
}

func unseen(seen bool) string {
	if seen {
		return ""
	}
	return ", which tofu did not see"
}
