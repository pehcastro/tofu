package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/judge/question"
	"tofu/internal/judge/state"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/memory"
	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	OutcomeKindMemoryScope = "memory-scope"
	refusedLocal           = "refused project local: every teammate reads it, and it names something of the person's, or Jev could not say it does not. offer it as user-local or project-global instead"
)

type ScopeJudge struct {
	Client *jev.Client
	Set    question.Set
	Ledger *ledger.Writer
}

type scopeJudgement struct {
	row          ledger.Row
	pick         memory.Scope
	refusesLocal bool
}

func (j *ScopeJudge) judge(ctx context.Context, entry memory.Entry, project string) scopeJudgement {
	if j == nil {
		return scopeJudgement{refusesLocal: true}
	}
	full, _ := filepath.Abs(project)
	ignored := exec.CommandContext(ctx, "git", "-C", full, "check-ignore", "-q", sys.StateDir(full)).Run() == nil
	body, builder, err := state.BuildMemoryScope(state.MemoryScopeState{Candidate: entry.Text, Said: entry.Said, Repository: filepath.Base(full), RepositoryTofuIgnored: ignored})
	if err != nil {
		return scopeJudgement{refusesLocal: true}
	}
	shadow := "the person's pick on the card is the label; a names_private yes refuses project local"
	row := ledger.Row{Point: state.MemoryScopePoint, Questions: j.Set.Name, Version: j.Set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: builder, State: body,
		Reason: &ledger.Reason{Question: state.MemoryNamesPrivateQuestion, Comparison: ">= its no", Mode: ledger.ModeShadow, ModeReason: &shadow}}
	asked := make([]jev.Question, len(j.Set.Questions))
	for i, q := range j.Set.Questions {
		asked[i] = q.ToJev()
	}
	decision, err := j.Client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: asked})
	if err != nil {
		failed := "jev could not answer, so project local is refused: " + err.Error()
		row.Reason.ModeReason = &failed
		return scopeJudgement{row: row, refusesLocal: true}
	}
	row.Build, row.Model, row.RequestID = decision.Build, decision.Alias, decision.RequestID
	row.LatencyMS, row.Cost = decision.Latency.Milliseconds(), decision.Usage.Cost
	for _, q := range j.Set.Questions {
		answer := decision.Answers[q.Name]
		recorded := ledger.Answer{Question: q.Name, Wording: j.Set.QuestionsVersion, Kind: ledger.AnswerNoul, Noul: answer.Noul}
		if q.Kind == question.KindChoice {
			recorded = ledger.Answer{Question: q.Name, Wording: j.Set.QuestionsVersion, Kind: ledger.AnswerChoice, Choice: answer.Choice, Dist: distOf(answer.Probabilities)}
		}
		row.Answers = append(row.Answers, recorded)
	}
	private := decision.Answers[state.MemoryNamesPrivateQuestion].Noul
	row.Reason.Value, row.Reason.Threshold = private, 1-private
	judged := scopeJudgement{row: row, refusesLocal: state.RefusesProjectLocal(private, true)}
	if picked, err := memory.ParseScope(state.MemoryScopeOf(decision.Answers[state.MemoryScopeQuestion].Choice)); err == nil && (picked != memory.ProjectLocal || !judged.refusesLocal) {
		judged.pick = picked
	}
	return judged
}

func (j *ScopeJudge) record(judged scopeJudgement, verdict ledger.Verdict) (string, error) {
	if j == nil {
		return "", nil
	}
	judged.row.Verdict = verdict
	written, err := j.Ledger.Append(judged.row)
	return written.ID, err
}

func (j *ScopeJudge) label(row, pick string) error {
	if row == "" {
		return nil
	}
	return j.Ledger.Backfill(row, ledger.Outcome{Kind: OutcomeKindMemoryScope, Detail: pick})
}

type MemoryOffer struct {
	Statement string
	Said      string
	Scope     memory.Scope
	Scopes    []memory.Scope
}

type MemoryPick func(ctx context.Context, offer MemoryOffer) (memory.Scope, error)

type memoryPickKey struct{}

func WithMemoryPick(ctx context.Context, pick MemoryPick) context.Context {
	return context.WithValue(ctx, memoryPickKey{}, pick)
}

type Remember struct {
	Store   *session.Store
	Session string
	Project string
	Inbox   *turn.Inbox
	Auto    func() bool
	Judge   *ScopeJudge
}

func (Remember) Name() string { return turn.RememberToolName }

func (Remember) Definition() llm.Tool {
	return llm.Tool{
		Name: turn.RememberToolName,
		Description: "offers the person to keep one short rule for every later session. call it only for a standing rule the person stated, such as a correction or how things are always done here; never to save a whole message, a task, or a summary. " +
			"said must be their own words, copied exactly from a message they typed in this conversation; anything else is refused. " +
			"statement is the rule itself and nothing else: one line, at most " + strconv.Itoa(konst.MemoryRuleBytes) + " bytes, imperative or declarative, naming no one: no name, no the person, the user, he or she, such as: " +
			"Desk UI primitives are widened to fit a new need; never build a parallel copy in a caller. a longer statement, a second line, or one that names the person is refused with the reason. " +
			"the person answers yes, no, or always, and picks one of the four scopes on the card; nothing is kept before a yes. scope says where it belongs: user-global the person everywhere, project-global this project in the person's home, user-local the person in this repository, project-local the team in this repository. without scope a person entry goes to user-global, a project or reference entry to project-global",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"statement": map[string]any{"type": "string", "description": "the rule itself, one line of at most " + strconv.Itoa(konst.MemoryRuleBytes) + " bytes, naming no one"},
				"said":      map[string]any{"type": "string", "description": "the person's exact words this comes from"},
				"kind":      map[string]any{"type": "string", "enum": []string{string(memory.KindPerson), string(memory.KindProject), string(memory.KindReference)}},
				"scope":     map[string]any{"type": "string", "enum": []string{string(memory.UserLocal), string(memory.ProjectLocal), string(memory.Project), string(memory.Global)}},
			},
			"required": []string{"statement", "said", "kind"},
		},
	}
}

func (r Remember) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Statement string       `json:"statement"`
		Said      string       `json:"said"`
		Kind      memory.Kind  `json:"kind"`
		Scope     memory.Scope `json:"scope"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("remember: arguments are not the expected shape: %w", err)
	}
	rule, err := memory.Rule(args.Statement)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	typed, err := r.typedInChain()
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	quote := strings.Join(strings.Fields(args.Said), " ")
	if !slices.ContainsFunc(typed, func(words string) bool { return quote != "" && strings.Contains(words, quote) }) {
		return turn.Result{}, fmt.Errorf("remember: %q is not in any message the person typed in this conversation; copy their words exactly, or do not offer it", args.Said)
	}
	entry := memory.Entry{Scope: cmp.Or(args.Scope, args.Kind.Scope()), Kind: args.Kind, Text: rule, Said: args.Said, Session: r.Session, At: time.Now(), By: memory.ByLead}
	if family, err := r.Store.Identity(r.Session); err == nil {
		entry.Session = family.Family
	}
	judged := r.Judge.judge(ctx, entry, r.Project)
	refused := entry.Scope == memory.ProjectLocal && judged.refusesLocal
	asks := !r.Auto() || entry.Scope == memory.ProjectLocal
	verdict := ledger.VerdictAllow
	switch {
	case refused:
		verdict = ledger.VerdictDeny
	case asks:
		verdict = ledger.VerdictAsk
	}
	row, err := r.Judge.record(judged, verdict)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: the %s row did not write: %w", state.MemoryScopePoint, err)
	}
	pick, _ := ctx.Value(memoryPickKey{}).(MemoryPick)
	switch {
	case refused:
		return turn.Result{Content: refusedLocal}, nil
	case asks && pick == nil:
		return turn.Result{Content: "no person is here to answer, so nothing was kept"}, nil
	case asks:
		scopes := []memory.Scope{memory.Global, memory.UserLocal, memory.Project, memory.ProjectLocal}
		if judged.refusesLocal {
			scopes = scopes[:3]
		}
		picked, err := pick(ctx, MemoryOffer{Statement: entry.Text, Said: args.Said, Scope: cmp.Or(judged.pick, entry.Scope), Scopes: scopes})
		if err != nil {
			return turn.Result{Content: "the person could not be asked, so nothing was kept: " + err.Error()}, nil
		}
		if err := r.Judge.label(row, cmp.Or(string(picked), state.MemoryScopeNone)); err != nil {
			return turn.Result{}, fmt.Errorf("remember: the person's pick did not reach the ledger, so nothing was kept: %w", err)
		}
		switch {
		case picked == "":
			return turn.Result{Content: "the person said no, and nothing was kept"}, nil
		case !slices.Contains(scopes, picked):
			return turn.Result{Content: refusedLocal}, nil
		}
		entry.Scope = picked
	}
	shelves, err := memory.Open(r.Project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	if entry, err = shelves.Add(entry, ""); err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	r.Inbox.Remembered(entry.Saved())
	return turn.Result{Content: entry.Saved(), Command: entry.Ref()}, nil
}

func (r Remember) typedInChain() ([]string, error) {
	newest, err := r.Store.Header(r.Session)
	for err == nil && newest.ForkedInto != "" && newest.ForkedInto != newest.ID {
		newest, err = r.Store.Header(newest.ForkedInto)
	}
	if err != nil {
		return nil, err
	}
	ancestors, err := r.Store.Ancestors(newest.ID)
	if err != nil {
		return nil, err
	}
	var typed []string
	for _, header := range append([]session.Header{newest}, ancestors...) {
		reading, err := r.Store.Reading(header.ID)
		if err != nil {
			return nil, err
		}
		for _, message := range reading.Messages {
			if words, said := turn.TaskIn(message.Content); said && message.Role == session.RoleUser && turn.SaidByThePerson(message.Origin) {
				typed = append(typed, strings.Join(strings.Fields(words), " "))
			}
		}
	}
	return typed, nil
}

func distOf(probabilities map[string]float64) []ledger.Slice {
	dist := make([]ledger.Slice, 0, len(probabilities))
	for _, option := range slices.Sorted(maps.Keys(probabilities)) {
		dist = append(dist, ledger.Slice{Option: option, P: probabilities[option]})
	}
	return dist
}
