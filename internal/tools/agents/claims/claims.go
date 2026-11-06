package claims

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/judge/ledger"
	question "tofu/internal/judge/questions"
	"tofu/internal/konst"
	jev "tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/context/window"
	"tofu/internal/tools/contract"
	"tofu/library/questions"
)

const (
	Name         = "claims"
	questionsRef = "claims@2"
	stateBuilder = "claims_state@1"
	noEvidence   = "none"
)

type Verdict string

const (
	Shown        Verdict = "shown"
	NotShown     Verdict = "not_shown"
	Contradicted Verdict = "contradicted"
)

type Claims struct {
	Sessions *session.Store
	Dir      string
	Ledger   *ledger.Writer
	Open     func() (*jev.Client, error)
}

type call struct {
	Name     string `json:"name"`
	Tool     string `json:"tool"`
	Args     string `json:"args"`
	ExitCode *int   `json:"exit_code"`
	Outcome  string `json:"outcome"`
	Error    string `json:"error"`
	Output   string `json:"output"`
	id       string
}

type state struct {
	Parts []string `json:"parts"`
	Calls []call   `json:"calls"`
}

type checked struct {
	text, evidence string
	verdict        Verdict
}

var partBreak = regexp.MustCompile(`[;!?\n]|\.(?:\s|$)`)

func (Claims) Name() string { return Name }

func (c Claims) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "checks each claim in a report you are about to give against the tool calls your session made, before you give it. " +
			"report is the text; each sentence that states a fact a tool call could show reads shown with the call id that shows it, " +
			"not_shown when no call does, or contradicted with the call whose output says it is false. " +
			fmt.Sprintf("the first %d sentences are checked against the latest %d calls. session names another recorded session by name, id or head; left out it is the one you run in.", konst.ClaimsPartsChecked, konst.ClaimsCallsOffered),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"report":  map[string]any{"type": "string"},
				"session": map[string]any{"type": "string"},
			},
			"required": []string{"report"},
		},
	}
}

func (c Claims) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Report  string `json:"report"`
		Session string `json:"session"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("claims: arguments are not the expected shape: %w", err)
	}
	var parts []string
	for _, part := range partBreak.Split(args.Report, -1) {
		if part = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(part), "-*")); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return contract.Result{Content: "0 claims: the report holds no sentence\n", Command: Name}, nil
	}
	unchecked := max(0, len(parts)-konst.ClaimsPartsChecked)
	parts = parts[:len(parts)-unchecked]
	asked, err := window.Ask(ctx, c.Sessions, args.Session)
	if err != nil {
		return contract.Result{}, fmt.Errorf("claims: %w", err)
	}
	set, fields, err := c.questions()
	if err != nil {
		return contract.Result{}, fmt.Errorf("claims: %w", err)
	}
	thresholds := map[string]float64{}
	for _, name := range []string{"claim_at", "shown_at", "contradicted_at"} {
		at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+name })
		if at >= 0 {
			thresholds[name], err = strconv.ParseFloat(fields[at].Value, 64)
		}
		if at < 0 || err != nil || thresholds[name] <= 0 || thresholds[name] > 1 {
			return contract.Result{}, fmt.Errorf("claims: %s declares no thresholds.%s above 0 and at or below 1", set.File, name)
		}
	}
	client, err := c.Open()
	if err != nil {
		return contract.Result{}, fmt.Errorf("claims: no claim is checked, because Jev cannot be reached: %w", err)
	}
	calls := callsOf(asked.Events, min(konst.ClaimsCallsOffered, client.Caps().MaxChoiceOptions-1))
	var asking []jev.Question
	for i := range parts {
		for _, q := range set.Questions {
			if len(calls) == 0 && q.Name != "claim" {
				continue
			}
			each := q.ToJev()
			each.ID, each.Instructions = q.Name+"_"+strconv.Itoa(i+1), each.Instructions+" The part is parts["+strconv.Itoa(i)+"]."
			if each.Kind == jev.QuestionChoice {
				for _, offered := range calls {
					each.Options = append(each.Options, jev.Option{Name: offered.Name, Criteria: offered.Tool + " " + offered.Args})
				}
			}
			asking = append(asking, each)
		}
	}
	body, err := json.Marshal(state{Parts: parts, Calls: calls})
	if err != nil {
		return contract.Result{}, fmt.Errorf("claims: %w", err)
	}
	row := ledger.Row{Point: Name, Questions: set.Name, Version: set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: stateBuilder, State: body,
		Verdict: ledger.VerdictAsk, TurnID: asked.Turn, Reason: &ledger.Reason{Question: "contradicts", Comparison: "contradicted_at", Threshold: thresholds["contradicted_at"], Mode: ledger.ModeEnforced}}
	decision, err := client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asking})
	if err != nil {
		cause := "jev could not answer, so no claim is checked: " + err.Error()
		row.Reason.ModeReason = &cause
		_, unwritten := c.Ledger.Append(row)
		return contract.Result{}, errors.Join(errors.New("claims: "+cause), unwritten)
	}
	decision.Record(&row, asking, set.QuestionsVersion)
	var found []checked
	for i, part := range parts {
		n := strconv.Itoa(i + 1)
		if decision.Answers["claim_"+n].Noul < thresholds["claim_at"] {
			continue
		}
		contradicts := decision.Answers["contradicts_"+n]
		if contradicts.Choice != noEvidence {
			row.Reason.Value = max(row.Reason.Value, contradicts.Probabilities[contradicts.Choice])
		}
		shownBy, contradictedBy := chosen(calls, decision.Answers["evidence_"+n], thresholds["shown_at"]), chosen(calls, contradicts, thresholds["contradicted_at"])
		if contradictedBy >= 0 && slices.ContainsFunc(calls[contradictedBy+1:], func(later call) bool {
			return later.Tool == calls[contradictedBy].Tool && later.Args == calls[contradictedBy].Args && later.Outcome == session.ToolOutcomeRan
		}) {
			contradictedBy = -1
		}
		claim := checked{text: part, evidence: noEvidence, verdict: NotShown}
		switch {
		case contradictedBy > shownBy:
			claim.verdict, claim.evidence = Contradicted, calls[contradictedBy].id
		case shownBy > contradictedBy:
			claim.verdict, claim.evidence = Shown, calls[shownBy].id
		}
		found = append(found, claim)
	}
	row.Verdict = ledger.VerdictAllow
	for _, claim := range found {
		switch claim.verdict {
		case Contradicted:
			row.Verdict = ledger.VerdictDeny
		case NotShown:
			if row.Verdict != ledger.VerdictDeny {
				row.Verdict = ledger.VerdictAsk
			}
		case Shown:
		}
	}
	if _, err := c.Ledger.Append(row); err != nil {
		return contract.Result{}, fmt.Errorf("claims: the decision did not reach the ledger, so no claim is checked: %w", err)
	}
	var said strings.Builder
	fmt.Fprintf(&said, "%d claims in %d parts of the report, checked against %d calls of session %s\n", len(found), len(parts), len(calls), cmp.Or(asked.Handle, "none"))
	for _, claim := range found {
		fmt.Fprintf(&said, "  %s %s: %s\n", claim.verdict, claim.evidence, claim.text)
	}
	if unchecked > 0 {
		fmt.Fprintf(&said, "%d parts past the first %d are not checked\n", unchecked, konst.ClaimsPartsChecked)
	}
	return contract.Result{Content: said.String(), Command: Name}, nil
}

func (c Claims) questions() (question.Set, []question.Field, error) {
	layers, err := question.Layers(questions.Files(), c.Dir)
	if err != nil {
		return question.Set{}, nil, err
	}
	return question.Resolve(questionsRef, layers)
}

func callsOf(events []session.Event, keep int) []call {
	var calls []call
	for _, event := range events {
		switch event.Kind {
		case session.EventToolCall:
			var body session.CallBody
			if json.Unmarshal(event.Body, &body) != nil || body.Tool == Name {
				continue
			}
			calls = append(calls, call{Tool: body.Tool, Args: tail(string(body.Args), konst.ClaimsArgsBytes), id: event.Call})
		case session.EventToolResult:
			var body session.ResultBody
			at := len(calls) - 1
			for at >= 0 && calls[at].id != event.Call {
				at--
			}
			if at < 0 || json.Unmarshal(event.Body, &body) != nil {
				continue
			}
			calls[at].ExitCode, calls[at].Outcome, calls[at].Error, calls[at].Output = body.ExitCode, body.ToolOutcome, body.Error, tail(body.Content, konst.ClaimsOutputBytes)
		}
	}
	calls = calls[max(0, len(calls)-max(0, keep)):]
	for i := range calls {
		calls[i].Name = "call_" + strconv.Itoa(i+1)
	}
	return calls
}

func tail(text string, bytes int) string {
	return strings.ToValidUTF8(text[max(0, len(text)-bytes):], "")
}

func chosen(calls []call, answer jev.Answer, at float64) int {
	if answer.Probabilities[answer.Choice] < at {
		return -1
	}
	return slices.IndexFunc(calls, func(c call) bool { return c.Name == answer.Choice })
}
