package turn

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/recall"
	"tofu/internal/session"
)

type sizedRead struct{ bytes map[string]int }

func (sizedRead) Name() string { return "read" }

func (sizedRead) Definition() llm.Tool {
	return llm.Tool{Name: "read", Parameters: map[string]any{"type": "object"}}
}

func (r sizedRead) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return Result{}, err
	}
	line := "the text of " + args.Path + "\n"
	return Result{Content: strings.Repeat(line, max(1, r.bytes[args.Path]/len(line)))}, nil
}

type scriptedLead struct {
	steps    func(step int) llm.Decision
	state    string
	stateErr error
	asked    []llm.Request
	stated   []int
	loud     bool
}

const stateCacheRead = 4321

func (m *scriptedLead) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	m.asked = append(m.asked, request)
	if request.Messages[len(request.Messages)-1].Origin.Source == sourceForkState {
		m.stated = append(m.stated, len(m.asked)-1)
		m.loud = m.loud || !AskedQuietly(ctx)
		return llm.Decision{Build: "m1", Outcome: llm.OutcomeMessage, Content: m.state, CacheReadTokens: stateCacheRead}, m.stateErr
	}
	m.loud = m.loud || AskedQuietly(ctx)
	return m.steps(len(m.asked) - len(m.stated)), nil
}

func readStep(step int) llm.Decision {
	id := "call-" + strconv.Itoa(step)
	calls := []llm.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"f` + strconv.Itoa(step) + `.go"}`)}}
	if step == 1 {
		calls = append(calls, llm.ToolCall{ID: "call-sh", Name: "bash", Arguments: json.RawMessage(`{"command":"cat > a.sh <<'EOF'\necho one\necho two\nEOF"}`)},
			llm.ToolCall{ID: "call-w", Name: "write", Arguments: json.RawMessage(`{"path":"b.go","content":"package b"}`)})
	}
	return toolCallDecision(calls...)
}

func forkingConfig(t *testing.T, model Model, bytes map[string]int, ceiling string) (Config, *[]StepRow) {
	t.Helper()
	t.Setenv(recall.CeilingVariable, ceiling)
	budget, err := recall.BudgetFor("m1", 0)
	if err != nil {
		t.Fatal(err)
	}
	var steps []StepRow
	config := baseConfig(t, model, NewRegistry(sizedRead{bytes: bytes}, &stubTool{name: "bash", result: Result{Content: "wrote a.sh"}},
		&stubTool{name: "write", result: Result{Content: "wrote b.go"}}))
	config.Task, config.Budget, config.Caps.MaxSteps, config.ResultBytesCap = "rename the parser", budget, 30, 1<<20
	config.Step = func(step StepRow) { steps = append(steps, step) }
	return config, &steps
}

func TestAForkCarriesTheLeadsOwnStateAndPointsAtCallsLookupReadsBackFromAnyGeneration(t *testing.T) {
	const said = "goal: rename the parser\nnext: read f9.go"
	for _, c := range []struct {
		name     string
		state    string
		stateErr error
	}{
		{"the lead answers with a state longer than the cap", said + "\n" + strings.Repeat("more ", konst.ForkStateBytes), nil},
		{"the state request fails", "", errors.New("the wire is down")},
	} {
		t.Run(c.name, func(t *testing.T) {
			bytes := map[string]int{}
			for i := range 16 {
				bytes["f"+strconv.Itoa(i+1)+".go"] = 3000
			}
			model := &scriptedLead{state: c.state, stateErr: c.stateErr, steps: func(step int) llm.Decision {
				if step > 14 {
					return messageDecision()
				}
				return readStep(step)
			}}
			config, steps := forkingConfig(t, model, bytes, "12000")
			store, id := session.NewStore(t.TempDir()), session.NewEventID()
			config.Sessions, config.Session, config.NoCompaction = store, id, true
			row, err := Run(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			var forks []*Fork
			for _, step := range *steps {
				if step.Fork != nil {
					forks = append(forks, step.Fork)
				}
			}
			if len(forks) < 2 || len(model.stated) != len(forks) {
				t.Fatalf("%d forks and %d state requests, want two forks or more and one state request each", len(forks), len(model.stated))
			}
			for n, at := range model.stated {
				asked, before := model.asked[at], model.asked[at-1]
				if len(asked.Messages) <= len(before.Messages) || !slices.EqualFunc(before.Messages, asked.Messages[:len(before.Messages)], func(a, b llm.Message) bool {
					return a.Content == b.Content && a.ToolCallID == b.ToolCallID
				}) {
					t.Errorf("state request %d does not open on the request before it, so it cannot read that request's cache", n+1)
				}
				if asked.ToolChoice != llm.ToolChoiceNone || !slices.EqualFunc(asked.Tools, before.Tools, func(a, b llm.Tool) bool { return a.Name == b.Name }) {
					t.Errorf("state request %d sends tool choice %v and tools other than the step's", n+1, asked.ToolChoice)
				}
			}
			if model.loud {
				t.Error("a state request was asked where the person sees replies, or a step was asked quietly")
			}
			events, err := store.Events(id)
			if err != nil {
				t.Fatal(err)
			}
			recordedRead := slices.ContainsFunc(events, func(event session.Event) bool {
				var spent session.StepBody
				return event.Kind == session.EventRequest && json.Unmarshal(event.Body, &spent) == nil && spent.CacheReadTokens == stateCacheRead && spent.AssistantText == ""
			})
			if recordedRead == (c.stateErr != nil) {
				t.Errorf("the state request's cache read of %d is recorded %v, want it recorded when the request answered", stateCacheRead, recordedRead)
			}
			first, second := forks[0], forks[1]
			if !strings.HasPrefix(first.Carry.Text, "this is fork 1.\n") || !strings.HasPrefix(second.Carry.Text, "this is fork 2.\n") {
				t.Errorf("the forks do not count from the chain:\n%s\n---\n%s", first.Carry.Text, second.Carry.Text)
			}
			if c.stateErr == nil && (!strings.Contains(first.Carry.Text, said) || len(first.Carry.State) > konst.ForkStateBytes || first.Carry.State == "") {
				t.Errorf("the carry holds a state of %d bytes against a cap of %d, or not the lead's words:\n%s", len(first.Carry.State), konst.ForkStateBytes, first.Carry.Text)
			}
			if c.stateErr != nil && (first.Carry.State != "" || !slices.ContainsFunc(row.Warnings, func(w string) bool { return strings.Contains(w, "the wire is down") })) {
				t.Errorf("a failed state request left state %q and warnings %q", first.Carry.State, row.Warnings)
			}
			for _, want := range []string{"call lookup with its call id instead of reading or running it again", "session " + id, ", call call-1", "cat > a.sh <<'EOF' echo one"} {
				if !strings.Contains(first.Carry.Text, want) {
					t.Errorf("the first carry does not hold %q:\n%s", want, first.Carry.Text)
				}
			}
			if strings.Contains(first.Carry.Text, "echo one\n") || strings.Contains(first.Carry.Text, "b.go :: write") {
				t.Errorf("the first carry copies the script or lists the write:\n%s", first.Carry.Text)
			}
			after := model.asked[model.stated[0]+1]
			if !slices.ContainsFunc(after.Tools, func(tool llm.Tool) bool { return tool.Name == LookupToolName }) {
				t.Fatal("a turn that records its session is not offered lookup")
			}
			tasks := 0
			for _, message := range after.Messages {
				if strings.Contains(message.Content, config.Task) && message.Origin.Source != sourceForkCarry {
					tasks++
				}
			}
			if tasks != 1 || after.Messages[0].Content != config.Task {
				t.Errorf("the request after the fork holds the task %d times, and opens on %q", tasks, after.Messages[0].Content)
			}
			lookup := lookupTool{sessions: store, current: func() string { return row.Session }}
			found, err := lookup.Run(context.Background(), json.RawMessage(`{"call":"call-1"}`))
			if err != nil || !strings.Contains(found.Content, "the text of f1.go") || !strings.Contains(found.Content, `{"path":"f1.go"}`) {
				t.Errorf("lookup from the newest session %s did not reach call-1 in the first, %v:\n%.300s", row.Session, err, found.Content)
			}
			for _, wrong := range []string{`{"session":"` + row.Session + `","call":"call-404"}`, `{"session":"..","call":"call-1"}`} {
				if _, err := lookup.Run(context.Background(), json.RawMessage(wrong)); err == nil {
					t.Errorf("lookup %s answered", wrong)
				}
			}
		})
	}
}

func TestATrimOfResultsAlreadyReadRunsWhenTheBudgetIsCrossedAndTheForkOnlyIfItIsNotEnough(t *testing.T) {
	model := &scriptedLead{state: "unused", steps: func(step int) llm.Decision {
		if step > 8 {
			return messageDecision()
		}
		return readStep(step)
	}}
	bytes := map[string]int{"f1.go": 12000}
	for i := 2; i <= 8; i++ {
		bytes["f"+strconv.Itoa(i)+".go"] = 1500
	}
	config, steps := forkingConfig(t, model, bytes, "12000")
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	trimmed := 0
	for _, step := range *steps {
		if step.Fork != nil {
			t.Errorf("step %d forked, though the trim got under the target", step.Index)
		}
		if step.Compaction == nil {
			continue
		}
		trimmed++
		if step.Compaction.TokensAfter > config.Budget.Bands.Target() {
			t.Errorf("the trim at step %d left %d tokens against a %d target", step.Index, step.Compaction.TokensAfter, config.Budget.Bands.Target())
		}
		after := model.asked[step.Index].Messages
		if first := resultOf(after, "call-1"); len(first) >= 12000 || !strings.Contains(first, "artifact ") {
			t.Errorf("the oldest result, read at step 1, went out at %d bytes after the trim at step %d", len(first), step.Index)
		}
		if newest := resultOf(after, "call-"+strconv.Itoa(step.Index)); strings.Contains(newest, "artifact ") {
			t.Errorf("the trim at step %d cut the result the model has not read yet", step.Index)
		}
	}
	if trimmed != 1 || len(model.stated) != 0 {
		t.Errorf("%d trims and %d state requests, want one trim and no state request", trimmed, len(model.stated))
	}
	facts, _, err := recall.Distil(nil, historyOf(model.asked[len(model.asked)-1].Messages), konst.CarrySignpostBytes)
	pointed := func(line string) bool {
		return strings.HasPrefix(line, "fact: f1.go :: read") && strings.Contains(line, ", call call-1")
	}
	if err != nil || !slices.ContainsFunc(facts, pointed) {
		t.Errorf("a later fork would not point at the trimmed read of f1.go, %v:\n%s", err, strings.Join(facts, "\n"))
	}
	if slices.ContainsFunc(model.asked[0].Tools, func(tool llm.Tool) bool { return tool.Name == LookupToolName }) {
		t.Error("a turn that records no session is offered lookup")
	}
}

func TestAForkKeepsTheTaskOnceCountsOnFromTheChainAndNeverGrows(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	const task = "rename the parser"
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are tofu"},
		{Role: llm.RoleUser, Content: "this is fork 4.\nan older carry"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c0", Name: "read", Arguments: json.RawMessage(`{"path":"old.go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c0", Content: strings.Repeat("a line of the old parser\n", 120)},
		{Role: llm.RoleAssistant, Content: "the older turn is done"},
		{Role: llm.RoleUser, Content: task, Origin: llm.Origin{Source: sourceTask}},
		{Role: llm.RoleAssistant, Thinking: llm.Thinking{Text: "read a.go first", Signature: "signed"}, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "package a"},
	}
	fork, begun, err := forkHistory(artifacts, recall.Budget{}.At(1800, "a small budget"), task, messages, "", 1, 0)
	if err != nil || fork == nil {
		t.Fatalf("no fork, %v", err)
	}
	if !strings.HasPrefix(fork.Carry.Text, "this is fork 5.\n") {
		t.Errorf("a fork after fork 4 says:\n%s", fork.Carry.Text)
	}
	held := 0
	for _, message := range begun {
		if message.Content == task {
			held++
		}
		if !message.Thinking.Empty() {
			t.Errorf("the carried tail re-sends a signed thinking block: %+v", message.Thinking)
		}
	}
	if messages[6].Thinking.Empty() {
		t.Error("the fork stripped thinking from the conversation it forked from, not only from its copy")
	}
	if held != 1 || fork.TailMessages == 0 {
		t.Fatalf("the fork holds the task %d times with a tail of %d", held, fork.TailMessages)
	}
	if begun[1].Content != task || begun[2].Origin.Source != sourceForkCarry {
		t.Errorf("the forked conversation opens on %q and then %q, not the task and the carry", begun[1].Content, begun[2].Origin.Source)
	}
	tiny := []llm.Message{{Role: llm.RoleSystem, Content: strings.Repeat("you are tofu ", 900)}, {Role: llm.RoleUser, Content: task},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: "package a"}}
	if grown, _, err := forkHistory(artifacts, recall.Budget{}.At(3000, "a ceiling under the system prompt"), task, tiny, "", 1, 0); err != nil || grown != nil {
		t.Errorf("a fork from %d to %d tokens was taken, %v", grown.TokensBefore, grown.TokensAfter, err)
	}
}

type pictureRead struct{ calls int }

func (*pictureRead) Name() string { return "read" }

func (*pictureRead) Definition() llm.Tool {
	return llm.Tool{Name: "read", Parameters: map[string]any{"type": "object"}}
}

func (r *pictureRead) Run(context.Context, json.RawMessage) (Result, error) {
	r.calls++
	return Result{Content: "a.png: png, 4x4", Images: []llm.Image{{MediaType: "image/png", Data: []byte("png")}}, Repeat: r.calls > 1}, nil
}

func pictureStep(id string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + id + `.png"}`)}}},
		{Role: llm.RoleTool, ToolCallID: id, Content: id + ".png: png, 4x4", Images: []llm.Image{{MediaType: "image/png", Data: []byte(id)}}},
	}
}

func TestAPictureAToolReadsReachesTheModelCountsAndIsTheFirstThingDropped(t *testing.T) {
	model := &stubModel{decisions: []llm.Decision{toolCallDecision(llm.ToolCall{ID: "p1", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}),
		toolCallDecision(llm.ToolCall{ID: "p2", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}), messageDecision()}}
	config := baseConfig(t, model, NewRegistry(&pictureRead{}))
	if _, err := Run(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	if first := model.requests[1].Messages[len(model.requests[1].Messages)-1]; len(first.Images) != 1 {
		t.Fatalf("the picture the read returned is not on the tool message the model gets: %d images", len(first.Images))
	}
	if again := model.requests[2].Messages[len(model.requests[2].Messages)-1]; len(again.Images) != 0 {
		t.Errorf("a repeat of the same read sent the picture again")
	}

	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("a line of the parser\n", 400)
	messages := slices.Concat([]llm.Message{{Role: llm.RoleSystem, Content: "you are tofu"}, {Role: llm.RoleUser, Content: "look"}},
		pictureStep("old"), pictureStep("new"),
		[]llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
			{Role: llm.RoleTool, ToolCallID: "t1", Content: text}, {Role: llm.RoleAssistant, Content: "read them"}})
	withPictures := HistoryTokens(artifacts.preview, messages)
	budget, err := recall.BudgetFor("m1", 0)
	if err != nil {
		t.Fatal(err)
	}
	pictured, plain := budget.Tokens(artifacts.preview, historyOf(messages)), budget.Tokens(artifacts.preview, historyOf(stripped(messages)))
	if history := withPictures - HistoryTokens(artifacts.preview, stripped(messages)); pictured-plain != 2*konst.ImageTokens || history != 2*konst.ImageTokens {
		t.Errorf("two pictures add %d to the fork's estimate and %d to the history's, want %d", pictured-plain, history, 2*konst.ImageTokens)
	}
	shrink, err := shrinkTo(artifacts, messages, withPictures, withPictures-konst.ImageTokens+100, causeOverflow)
	if err != nil {
		t.Fatal(err)
	}
	if shrink.results != 1 || messages[3].Images != nil || !strings.Contains(messages[3].Content, pictureDropped) || len(messages[5].Images) != 1 || messages[7].Content != text {
		t.Errorf("a shrink that needed one picture's room dropped %d results: old picture %d, new picture %d, text kept %v", shrink.results, len(messages[3].Images), len(messages[5].Images), messages[7].Content == text)
	}

	var many []llm.Message
	for i := range konst.ImageResultsKept + 2 {
		many = append(many, pictureStep("p"+strconv.Itoa(i))...)
	}
	keepNewestPictures(many)
	pictures := 0
	for i, message := range many {
		pictures += len(message.Images)
		if message.Role == llm.RoleTool && i < 4 && message.Images != nil {
			t.Errorf("picture %d of %d is kept", i/2+1, konst.ImageResultsKept+2)
		}
	}
	if pictures != konst.ImageResultsKept {
		t.Errorf("%d pictures go out, want %d", pictures, konst.ImageResultsKept)
	}

	fork, begun, err := forkHistory(artifacts, recall.Budget{}.At(1800, "a small budget"), "look", slices.Concat(messages[:2], pictureStep("last")), ForkContinuation, 1, 0)
	if err != nil || fork == nil {
		t.Fatalf("no fork, %v", err)
	}
	for _, message := range begun {
		if message.Role == llm.RoleTool && len(message.Images) > 0 {
			t.Errorf("the fork carries the picture of %s", message.ToolCallID)
		}
	}
}

func stripped(messages []llm.Message) []llm.Message {
	plain := slices.Clone(messages)
	for i := range plain {
		plain[i].Images = nil
	}
	return plain
}

func TestTheCarrysLastWordIsWhatTheModelSaidNeverTheArgumentsOfItsCalls(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Repeat("echo a line of the generated script\n", 200)
	args, err := json.Marshal(map[string]string{"path": "gen.sh", "content": script})
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "you are tofu"},
		{Role: llm.RoleUser, Content: "write the generator"},
		{Role: llm.RoleAssistant, Content: "the generator writes one line per rule"},
		{Role: llm.RoleUser, Content: "go on"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "w1", Name: "write", Arguments: args}}},
		{Role: llm.RoleTool, ToolCallID: "w1", Content: "wrote gen.sh"},
	}
	fork, _, err := forkHistory(artifacts, recall.Budget{}.At(3000, "a budget the script crosses"), "go on", messages, "", 1, 0)
	if err != nil || fork == nil {
		t.Fatalf("no fork, %v", err)
	}
	if strings.Contains(fork.Carry.Text, "echo a line of the generated script") || !strings.Contains(fork.Carry.Text, "the last thing it said or did:\nit called write") {
		t.Errorf("the carry copies the script it wrote, or loses the last thing said:\n%.600s", fork.Carry.Text)
	}
}

func TestLookupStopsOnACarriedFromLoopAndSaysWhatItLookedFor(t *testing.T) {
	store := session.NewStore(t.TempDir())
	a, b := session.NewEventID(), session.NewEventID()
	for _, header := range []session.Header{{ID: a, CarriedFrom: &session.Carried{Session: b}}, {ID: b, CarriedFrom: &session.Carried{Session: a}}} {
		if err := store.Write(header, nil); err != nil {
			t.Fatal(err)
		}
	}
	_, err := lookupTool{sessions: store}.Run(context.Background(), json.RawMessage(`{"session":"`+a+`","call":"call-9"}`))
	if err == nil || !strings.Contains(err.Error(), "call-9") || !strings.Contains(err.Error(), a) {
		t.Fatalf("lookup over a loop answered %v", err)
	}
}
