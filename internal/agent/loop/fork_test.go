package loop

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/contract"
)

func taskOpening(task string) core.Message {
	return core.Message{Role: core.RoleUser, Content: task, Origin: core.Origin{Source: core.SourceForkTask}}
}

type sizedRead struct{ bytes map[string]int }

func (sizedRead) Name() string { return "read" }

func (sizedRead) Definition() core.Tool {
	return core.Tool{Name: "read", Parameters: map[string]any{"type": "object"}}
}

func (r sizedRead) Run(_ context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, err
	}
	line := "the text of " + args.Path + "\n"
	return contract.Result{Content: strings.Repeat(line, max(1, r.bytes[args.Path]/len(line)))}, nil
}

type scriptedLead struct {
	steps    func(step int) core.Decision
	state    string
	stateErr error
	asked    []core.Request
	stated   []int
	loud     bool
}

const stateCacheRead = 4321

func (m *scriptedLead) Ask(ctx context.Context, request core.Request) (core.Decision, error) {
	m.asked = append(m.asked, request)
	if request.Messages[len(request.Messages)-1].Origin.Source == core.SourceForkState {
		m.stated = append(m.stated, len(m.asked)-1)
		m.loud = m.loud || !AskedQuietly(ctx)
		return core.Decision{Build: "m1", Outcome: core.OutcomeMessage, Content: m.state, CacheReadTokens: stateCacheRead}, m.stateErr
	}
	m.loud = m.loud || AskedQuietly(ctx)
	return m.steps(len(m.asked) - len(m.stated)), nil
}

func readStep(step int) core.Decision {
	id := "call-" + strconv.Itoa(step)
	calls := []core.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"f` + strconv.Itoa(step) + `.go"}`)}}
	if step == 1 {
		calls = append(calls, core.ToolCall{ID: "call-sh", Name: "bash", Arguments: json.RawMessage(`{"command":"cat > a.sh <<'EOF'\necho one\necho two\nEOF"}`)},
			core.ToolCall{ID: "call-w", Name: "write", Arguments: json.RawMessage(`{"path":"b.go","content":"package b"}`)})
	}
	return toolCallDecision(calls...)
}

func forkingConfig(t *testing.T, model Model, bytes map[string]int, ceiling string) (Config, *[]StepRow) {
	t.Helper()
	t.Setenv(recall.CeilingVariable, ceiling)
	budget, err := recall.BudgetUnder("m1", 0, konst.ContextCeilingTokens)
	if err != nil {
		t.Fatal(err)
	}
	var steps []StepRow
	config := baseConfig(t, model, NewRegistry(sizedRead{bytes: bytes}, &stubTool{name: "bash", result: contract.Result{Content: "wrote a.sh"}},
		&stubTool{name: "write", result: contract.Result{Content: "wrote b.go"}}))
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
			model := &scriptedLead{state: c.state, stateErr: c.stateErr, steps: func(step int) core.Decision {
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
				if len(asked.Messages) <= len(before.Messages) || !slices.EqualFunc(before.Messages, asked.Messages[:len(before.Messages)], func(a, b core.Message) bool {
					return a.Content == b.Content && a.ToolCallID == b.ToolCallID
				}) {
					t.Errorf("state request %d does not open on the request before it, so it cannot read that request's cache", n+1)
				}
				if asked.ToolChoice != core.ToolChoiceNone || !slices.EqualFunc(asked.Tools, before.Tools, func(a, b core.Tool) bool { return a.Name == b.Name }) {
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
			for _, want := range []string{"call history with mode lookup and the call id as its anchor instead of reading or running it again", "session " + id, ", call call-1", "cat > a.sh <<'EOF' echo one"} {
				if !strings.Contains(first.Carry.Text, want) {
					t.Errorf("the first carry does not hold %q:\n%s", want, first.Carry.Text)
				}
			}
			if strings.Contains(first.Carry.Text, "echo one\n") || strings.Contains(first.Carry.Text, "b.go :: write") {
				t.Errorf("the first carry copies the script or lists the write:\n%s", first.Carry.Text)
			}
			after := model.asked[model.stated[0]+1]
			if !slices.ContainsFunc(after.Tools, func(tool core.Tool) bool { return tool.Name == HistoryToolName }) {
				t.Fatal("a turn that records its session is not offered history")
			}
			tasks := 0
			for _, message := range after.Messages {
				if strings.Contains(message.Content, config.Task) && message.Origin.Source != core.SourceForkCarry {
					tasks++
				}
			}
			if tasks != 1 || after.Messages[0].Content != config.Task {
				t.Errorf("the request after the fork holds the task %d times, and opens on %q", tasks, after.Messages[0].Content)
			}
			lookup := historyTool{sessions: store, current: func() string { return row.Session }}
			found, err := lookup.Run(context.Background(), json.RawMessage(`{"mode":"lookup","anchor":"call-1"}`))
			if err != nil || !strings.Contains(found.Content, "the text of f1.go") || !strings.Contains(found.Content, `{"path":"f1.go"}`) {
				t.Errorf("lookup from the newest session %s did not reach call-1 in the first, %v:\n%.300s", row.Session, err, found.Content)
			}
			for _, wrong := range []string{`{"mode":"lookup","session":"` + row.Session + `","anchor":"call-404"}`, `{"mode":"lookup","session":"..","anchor":"call-1"}`} {
				if _, err := lookup.Run(context.Background(), json.RawMessage(wrong)); err == nil {
					t.Errorf("lookup %s answered", wrong)
				}
			}
		})
	}
}

func TestATrimOfResultsAlreadyReadRunsWhenTheBudgetIsCrossedAndTheForkOnlyIfItIsNotEnough(t *testing.T) {
	model := &scriptedLead{state: "unused", steps: func(step int) core.Decision {
		if step > 8 {
			return messageDecision()
		}
		return readStep(step)
	}}
	bytes := map[string]int{}
	config, steps := forkingConfig(t, model, bytes, "12000")
	shipped, err := recall.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	bytesOf := func(tokens int) int { return tokens * shipped.OnWire(config.Wire).BytesPerThousandTokens / 1000 }
	bytes["f1.go"] = bytesOf(5200)
	for i := 2; i <= 8; i++ {
		bytes["f"+strconv.Itoa(i)+".go"] = bytesOf(650)
	}
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
	if slices.ContainsFunc(model.asked[0].Tools, func(tool core.Tool) bool { return tool.Name == HistoryToolName }) {
		t.Error("a turn that records no session is offered history")
	}
}

func TestAForkKeepsTheTaskOnceCountsOnFromTheChainAndNeverGrows(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	const task = "rename the parser"
	messages := []core.Message{
		{Role: core.RoleSystem, Content: "you are tofu"},
		{Role: core.RoleUser, Content: "this is fork 4.\nan older carry"},
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: "c0", Name: "read", Arguments: json.RawMessage(`{"path":"old.go"}`)}}},
		{Role: core.RoleTool, ToolCallID: "c0", Content: strings.Repeat("a line of the old parser\n", 120)},
		{Role: core.RoleAssistant, Content: "the older turn is done"},
		{Role: core.RoleUser, Content: task, Origin: core.Origin{Source: core.SourceTask}},
		{Role: core.RoleAssistant, Thinking: core.Thinking{Text: "read a.go first", Signature: "signed"}, ToolCalls: []core.ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
		{Role: core.RoleTool, ToolCallID: "c1", Content: "package a"},
	}
	fork, begun, err := forkHistory(artifacts, recall.Budget{}.At(1800, "a small budget"), taskOpening(task), messages, "", 1, 0)
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
	if begun[1].Content != task || begun[2].Origin.Source != core.SourceForkCarry {
		t.Errorf("the forked conversation opens on %q and then %q, not the task and the carry", begun[1].Content, begun[2].Origin.Source)
	}
	tiny := []core.Message{{Role: core.RoleSystem, Content: strings.Repeat("you are tofu ", 900)}, {Role: core.RoleUser, Content: task},
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
		{Role: core.RoleTool, ToolCallID: "c1", Content: "package a"}}
	if grown, _, err := forkHistory(artifacts, recall.Budget{}.At(3000, "a ceiling under the system prompt"), taskOpening(task), tiny, "", 1, 0); err != nil || grown != nil {
		t.Errorf("a fork from %d to %d tokens was taken, %v", grown.TokensBefore, grown.TokensAfter, err)
	}
}

type pictureRead struct{ calls int }

func (*pictureRead) Name() string { return "read" }

func (*pictureRead) Definition() core.Tool {
	return core.Tool{Name: "read", Parameters: map[string]any{"type": "object"}}
}

func (r *pictureRead) Run(context.Context, json.RawMessage) (contract.Result, error) {
	r.calls++
	return contract.Result{Content: "a.png: png, 4x4", Images: []core.Image{{MediaType: "image/png", Data: []byte("png")}}, Repeat: r.calls > 1}, nil
}

func pictureStep(id string) []core.Message {
	return []core.Message{
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"` + id + `.png"}`)}}},
		{Role: core.RoleTool, ToolCallID: id, Content: id + ".png: png, 4x4", Images: []core.Image{{MediaType: "image/png", Data: []byte(id)}}},
	}
}

func TestAPictureAToolReadsReachesTheModelCountsAndIsTheFirstThingDropped(t *testing.T) {
	model := &stubModel{decisions: []core.Decision{toolCallDecision(core.ToolCall{ID: "p1", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}),
		toolCallDecision(core.ToolCall{ID: "p2", Name: "read", Arguments: json.RawMessage(`{"path":"a.png"}`)}), messageDecision()}}
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
	messages := slices.Concat([]core.Message{{Role: core.RoleSystem, Content: "you are tofu"}, {Role: core.RoleUser, Content: "look"}},
		pictureStep("old"), pictureStep("new"),
		[]core.Message{{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a.go"}`)}}},
			{Role: core.RoleTool, ToolCallID: "t1", Content: text}, {Role: core.RoleAssistant, Content: "read them"}})
	withPictures := HistoryTokens(artifacts.preview, messages)
	budget, err := recall.BudgetUnder("m1", 0, konst.ContextCeilingTokens)
	if err != nil {
		t.Fatal(err)
	}
	pictured, plain := budget.Tokens(artifacts.preview, historyOf(messages)), budget.Tokens(artifacts.preview, historyOf(stripped(messages)))
	if history := withPictures - HistoryTokens(artifacts.preview, stripped(messages)); pictured-plain != 2*konst.ImageTokens || history != 2*konst.ImageTokens {
		t.Errorf("two pictures add %d to the fork's estimate and %d to the history's, want %d", pictured-plain, history, 2*konst.ImageTokens)
	}
	shrink, err := shrinkFrom(t.Context(), artifacts, messages, withPictures, withPictures-konst.ImageTokens+100, 0, causeOverflow, "")
	if err != nil {
		t.Fatal(err)
	}
	if shrink.results != 1 || messages[3].Images != nil || !strings.Contains(messages[3].Content, pictureDropped) || len(messages[5].Images) != 1 || messages[7].Content != text {
		t.Errorf("a shrink that needed one picture's room dropped %d results: old picture %d, new picture %d, text kept %v", shrink.results, len(messages[3].Images), len(messages[5].Images), messages[7].Content == text)
	}

	var many []core.Message
	rewrites := 0
	for i := range 2 * konst.ImageResultsKept {
		sent := slices.Clone(many)
		many = append(many, pictureStep("p"+strconv.Itoa(i))...)
		keepNewestPictures(many)
		if !slices.EqualFunc(sent, many[:len(sent)], func(a, b core.Message) bool { return a.Content == b.Content && len(a.Images) == len(b.Images) }) {
			rewrites++
		}
		pictures := 0
		for _, message := range many {
			pictures += len(message.Images)
		}
		if pictures > konst.ImageResultsCeiling || pictures < min(i+1, konst.ImageResultsKept) || len(many[len(many)-1].Images) != 1 {
			t.Errorf("after picture %d, %d pictures go out, want between %d and %d with the newest among them", i+1, pictures, min(i+1, konst.ImageResultsKept), konst.ImageResultsCeiling)
		}
	}
	batch := konst.ImageResultsCeiling - konst.ImageResultsKept + 1
	if want := konst.ImageResultsKept / batch; rewrites > want {
		t.Errorf("%d pictures read one per call rewrote the cached prefix %d times, want at most %d: drop old pictures in batches, not one per call", 2*konst.ImageResultsKept, rewrites, want)
	}

	fork, begun, err := forkHistory(artifacts, recall.Budget{}.At(1800, "a small budget"), taskOpening("look"), slices.Concat(messages[:2], pictureStep("last")), ForkContinuation, 1, 0)
	if err != nil || fork == nil {
		t.Fatalf("no fork, %v", err)
	}
	for _, message := range begun {
		if message.Role == core.RoleTool && len(message.Images) > 0 {
			t.Errorf("the fork carries the picture of %s", message.ToolCallID)
		}
	}
}

func stripped(messages []core.Message) []core.Message {
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
	messages := []core.Message{
		{Role: core.RoleSystem, Content: "you are tofu"},
		{Role: core.RoleUser, Content: "write the generator"},
		{Role: core.RoleAssistant, Content: "the generator writes one line per rule"},
		{Role: core.RoleUser, Content: "go on"},
		{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: "w1", Name: "write", Arguments: args}}},
		{Role: core.RoleTool, ToolCallID: "w1", Content: "wrote gen.sh"},
	}
	fork, _, err := forkHistory(artifacts, recall.Budget{}.At(3000, "a budget the script crosses"), taskOpening("go on"), messages, "", 1, 0)
	if err != nil || fork == nil {
		t.Fatalf("no fork, %v", err)
	}
	if strings.Contains(fork.Carry.Text, "echo a line of the generated script") || !strings.Contains(fork.Carry.Text, "the last thing it said or did:\nit called write") {
		t.Errorf("the carry copies the script it wrote, or loses the last thing said:\n%.600s", fork.Carry.Text)
	}
}

func TestNoRequestOfAResumedConversationGoesPastTheCeiling(t *testing.T) {
	const ceiling = 12000
	said := func(bytes int) string {
		return strings.Repeat("the parser is renamed and every caller follows it. ", bytes/50)
	}
	brief := core.Message{Role: core.RoleUser, Content: "rename the parser", Origin: core.Origin{Source: core.SourceBrief}}
	words, reads := []core.Message{brief}, []core.Message{brief}
	for i := range 6 {
		id := "read-" + strconv.Itoa(i)
		words = append(words, core.Message{Role: core.RoleAssistant, Content: said(8000)}, core.Message{Role: core.RoleUser, Content: "go on"})
		reads = append(reads, core.Message{Role: core.RoleAssistant, ToolCalls: []core.ToolCall{{ID: id, Name: "read", Arguments: json.RawMessage(`{"path":"p.go"}`)}}},
			core.Message{Role: core.RoleTool, ToolCallID: id, Content: said(8000)})
	}
	preview, err := recall.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	preview = preview.OnWire("anthropic")
	for _, c := range []struct {
		name    string
		history []core.Message
		task    string
		refused bool
	}{
		{"its own words, which no trim frees", words, "go on with the rename", false},
		{"one answer longer than the ceiling", []core.Message{brief, {Role: core.RoleAssistant, Content: said(60000)}}, "go on with the rename", false},
		{"reads the trim frees", reads, "go on with the rename", false},
		{"a task longer than the ceiling", []core.Message{brief, {Role: core.RoleAssistant, Content: "renamed"}}, said(40000), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			model := &scriptedLead{state: "goal: rename the parser", steps: func(int) core.Decision { return messageDecision() }}
			config, _ := forkingConfig(t, model, nil, strconv.Itoa(ceiling))
			config.History, config.Task, config.SpawnedFrom = c.history, c.task, "lead"
			_, err := Run(context.Background(), config)
			if c.refused != (err != nil) || c.refused && !strings.Contains(err.Error(), "ceiling") {
				t.Errorf("the turn ended with %v, want a refusal naming the ceiling %v", err, c.refused)
			}
			if !c.refused && len(model.asked) == 0 {
				t.Fatal("no request was sent")
			}
			for n, request := range model.asked {
				schemas, err := json.Marshal(request.Tools)
				if err != nil {
					t.Fatal(err)
				}
				if sent := HistoryTokens(preview, request.Messages) + preview.Tokens(string(schemas)); sent > ceiling {
					t.Errorf("request %d of %d went out at about %d tokens, %d messages, past the %d ceiling", n+1, len(model.asked), sent, len(request.Messages), ceiling)
				}
			}
		})
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
	_, err := historyTool{sessions: store}.Run(context.Background(), json.RawMessage(`{"mode":"lookup","session":"`+a+`","anchor":"call-9"}`))
	if err == nil || !strings.Contains(err.Error(), "call-9") || !strings.Contains(err.Error(), a) {
		t.Fatalf("lookup over a loop answered %v", err)
	}
}
