package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"tofu/library/questions"
)

const (
	OutcomeKindMemoryScope = "memory-scope"
	memorySameName         = "same"
	memorySameRef          = "memory_same@1"
	memorySameBuilder      = "memory_same_state@1"
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
	decision.Record(&row, asked, j.Set.QuestionsVersion)
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

func (j *ScopeJudge) duplicate(ctx context.Context, candidate string, kept []memory.Entry, project string) (int, error) {
	if len(kept) == 0 {
		return -1, nil
	}
	if j == nil {
		return -1, errors.New("no jev")
	}
	layers, err := question.Layers(questions.Files(), project)
	if err != nil {
		return -1, err
	}
	set, fields, err := question.Resolve(memorySameRef, layers)
	if err != nil {
		return -1, err
	}
	asked, found := set.Question(memorySameName)
	at := slices.IndexFunc(fields, func(f question.Field) bool { return f.Path == "thresholds."+memorySameName+"_at" })
	if !found || at < 0 {
		return -1, fmt.Errorf("%s names no %s question or no thresholds.%s_at", memorySameRef, memorySameName, memorySameName)
	}
	threshold, err := strconv.ParseFloat(fields[at].Value, 64)
	if err != nil {
		return -1, err
	}
	type existing struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	asking := struct {
		Candidate string     `json:"candidate"`
		Existing  []existing `json:"existing"`
	}{Candidate: candidate}
	rubric := asked.ToJev()
	var each []jev.Question
	for i, e := range kept {
		asking.Existing = append(asking.Existing, existing{ID: e.ID, Text: e.Text})
		each = append(each, jev.Question{ID: memorySameName + "_" + strconv.Itoa(i), Kind: jev.QuestionNoul, Instructions: rubric.Instructions + " The existing rule is existing[" + strconv.Itoa(i) + "].", True: rubric.True, False: rubric.False})
	}
	body, err := json.Marshal(asking)
	if err != nil {
		return -1, err
	}
	row := ledger.Row{Point: set.Name, Questions: set.Name, Version: set.QuestionsVersion, StateHash: ledger.HashOf(body), StateBuilder: memorySameBuilder, State: body, Verdict: ledger.VerdictAllow,
		Reason: &ledger.Reason{Comparison: ">= " + fields[at].Value, Threshold: threshold, Mode: ledger.ModeEnforced}}
	decision, err := j.Client.Ask(ctx, jev.Request{State: json.RawMessage(body), Questions: each})
	if err != nil {
		failed := "jev could not answer, so the person is asked: " + err.Error()
		row.Verdict, row.Reason.ModeReason = ledger.VerdictAsk, &failed
		_, unwritten := j.Ledger.Append(row)
		return -1, errors.Join(err, unwritten)
	}
	decision.Record(&row, each, set.QuestionsVersion)
	same := -1
	for i, answer := range row.Answers {
		if answer.Noul >= threshold && (same < 0 || answer.Noul > row.Answers[same].Noul) {
			same = i
		}
	}
	if same >= 0 {
		row.Verdict, row.Reason.Question, row.Reason.Value = ledger.VerdictDeny, each[same].ID, row.Answers[same].Noul
	}
	_, err = j.Ledger.Append(row)
	return same, err
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

type MemoryKept struct {
	Statement string
	Offered   memory.Scope
	Kept      memory.Scope
}

type memoryKeptKey struct{}

func WithMemoryKept(ctx context.Context, told func(MemoryKept)) context.Context {
	return context.WithValue(ctx, memoryKeptKey{}, told)
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
			"the person answers yes, no, or always, and picks one of the four scopes on the card; nothing is kept before a yes. scope says where it belongs: user-global the person everywhere, project-global this project in the person's home, user-local the person in this repository, project-local the team in this repository. without scope a person entry goes to user-global, a project or reference entry to project-global. " +
			"a statement that says the same as an entry already kept is refused with that entry's id: call again with replaces set to that id to put this one in its place, or with keep_both true when the two really differ",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"statement": map[string]any{"type": "string", "description": "the rule itself, one line of at most " + strconv.Itoa(konst.MemoryRuleBytes) + " bytes, naming no one"},
				"said":      map[string]any{"type": "string", "description": "the person's exact words this comes from"},
				"kind":      map[string]any{"type": "string", "enum": []string{string(memory.KindPerson), string(memory.KindProject), string(memory.KindReference)}},
				"scope":     map[string]any{"type": "string", "enum": []string{string(memory.UserLocal), string(memory.ProjectLocal), string(memory.Project), string(memory.Global)}},
				"replaces":  map[string]any{"type": "string", "description": "the id of the entry this one takes the place of, as a refusal named it"},
				"keep_both": map[string]any{"type": "boolean", "description": "keep this entry beside the one a refusal named, because the two differ"},
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
		Replaces  string       `json:"replaces"`
		KeepBoth  bool         `json:"keep_both"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return turn.Result{}, fmt.Errorf("remember: arguments are not the expected shape: %w", err)
	}
	rule, err := memory.Rule(args.Statement)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	typed, err := r.Inbox.SaidInChain(r.Store, r.Session)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	quote := strings.Join(strings.Fields(args.Said), " ")
	if !slices.ContainsFunc(typed, func(said turn.PersonSaid) bool {
		return quote != "" && strings.Contains(strings.Join(strings.Fields(said.Words), " "), quote)
	}) {
		return turn.Result{}, fmt.Errorf("remember: %q is not in any message the person typed in this conversation; copy their words exactly, or do not offer it", args.Said)
	}
	entry := memory.Entry{Scope: cmp.Or(args.Scope, args.Kind.Scope()), Kind: args.Kind, Text: rule, Said: args.Said, Session: r.Session, At: time.Now(), By: memory.ByLead}
	if family, err := r.Store.Identity(r.Session); err == nil {
		entry.Session = family.Family
	}
	shelves, err := memory.Open(r.Project)
	if err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	kept := shelves.All()
	replaced := slices.IndexFunc(kept, func(e memory.Entry) bool { return e.ID == args.Replaces })
	unsure := false
	switch {
	case args.Replaces != "" && replaced < 0:
		return turn.Result{}, fmt.Errorf("remember: no memory entry is %s, so nothing replaces it", args.Replaces)
	case args.Replaces == "" && !args.KeepBoth:
		same, err := r.Judge.duplicate(ctx, rule, kept, r.Project)
		if same >= 0 {
			return turn.Result{Content: fmt.Sprintf("refused: this says the same as %s %q. call remember again with replaces %q to put this one in its place, or with keep_both true if the two differ", kept[same].Ref(), kept[same].Text, kept[same].ID)}, nil
		}
		unsure = err != nil
	}
	judged := r.Judge.judge(ctx, entry, r.Project)
	refused := entry.Scope == memory.ProjectLocal && judged.refusesLocal
	asks := !r.Auto() || entry.Scope == memory.ProjectLocal || unsure
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
	var swapped string
	if replaced >= 0 && kept[replaced].Scope == entry.Scope {
		swapped = kept[replaced].ID
	}
	if entry, err = shelves.Add(entry, swapped); err != nil {
		return turn.Result{}, fmt.Errorf("remember: %w", err)
	}
	if replaced >= 0 && swapped == "" {
		if _, err := shelves.Remove(kept[replaced].Scope, kept[replaced].ID); err != nil {
			return turn.Result{}, fmt.Errorf("remember: kept %s but %s is still there: %w", entry.ID, kept[replaced].ID, err)
		}
	}
	if told, telling := ctx.Value(memoryKeptKey{}).(func(MemoryKept)); telling && !asks {
		told(MemoryKept{Statement: entry.Text, Offered: cmp.Or(judged.pick, entry.Scope), Kept: entry.Scope})
	}
	r.Inbox.Remembered(entry.Saved())
	return turn.Result{Content: entry.Saved(), Command: entry.Ref()}, nil
}
