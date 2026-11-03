package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"tofu/interface/tui"
	"tofu/interface/tui/filmstrip"
	"tofu/interface/tui/fixture"
	"tofu/interface/tui/frame"
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/cred"
	"tofu/internal/llm/models"
	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	sessionstore "tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const driveUsage = `usage: tofu drive [SCRIPT] [--dir PATH] [--home PATH] [--cassette PATH] [--clipboard PATH] [--source NAME] [--quota PATH] [--width N] [--height N] [--timeout 60s] [--plain] [--fresh | --continue] [--host] [--setup] [ARM]

drives the app the way a person does, with no terminal and no model call.
SCRIPT is a file of steps, or - for standard input.

--source claude-sub|codex-sub names the subscription the orchestrator's model
reports, while the cassette still answers. Without it the source is cassette.

--setup reads the sign-ins and model bindings of the home, as tofu does when it
opens: it asks for what is missing and lists the models the home signed in to,
while the cassette still answers.

--quota PATH reads the footer's quota readings from a json file instead of
asking the vendors, which a fresh home cannot do:
  [{"label":"claude-sub 5h","fraction":0.62,"reported":true,"resetsIn":"3h28m"}]

The driven app never reads your clipboard. --clipboard PATH makes it hold that
file, as a file copied in the explorer does, so ctrl+v attaches it; without it
the clipboard is empty.

--host runs the session in the project's tofu host and the app joins it as a
member over the host's socket, as tofu does, so what a person types and pastes
crosses that socket. The host answers from the same cassette when this drive
starts it.

TOFU_DRIVE_REQUESTS names a file that every cassette, here, in the tofu host or
in a server, appends each request it was sent to, in the form requests prints,
so a run under the host still shows what the model was sent.

--fresh starts the app as a new session does: the chat, with the tofu cat and
wordmark in the empty transcript until the first message. --continue starts it
on the session you last worked in, as tofu --continue does. With neither the
app opens on an empty chat without the art.

An arm is one of tofu run's switches that change what the turn does rather than
where it reads from, and the app takes it exactly as tofu run does, so a
mechanism can be driven by hand on a cassette rather than argued about:
  --sift free|judged        which arm cuts a bash result before the model reads
                            it, otherwise the method table decides
  --done-review off|cheap|screen|typed   how a sub-agent's hand-back is
                            reviewed before the lead reads it, the doneReview
                            setting when not named
  --gate off|shadow|enforce, --no-gate    how a tool call is judged
  --tools full|three        which tools the turn is given
  --no-subagents            run without the spawn tool
  --no-instructions         send no AGENTS.md or CLAUDE.md
  --no-docs                 offer no tofu_docs tool and no sentence pointing at it
  --max-steps N             cap the steps a turn takes
  --context-ceiling N       the token ceiling the turn compacts against

Nothing here opens a wire by itself. --sift judged and a judged gate still ask
jev, so they need a key and fail closed without one, saying so on the screen.

Settings come from the script or from nowhere. Without --home the run makes an
empty home of its own, reads no settings file you own, and deletes that home
when it ends, so every setting is its declared default and two runs of one
script answer the same. --home PATH reads PATH/.tofu/settings.json instead,
which is how you drive the app against a configuration you keep. Your real
home, a folder above it, its .tofu, or a folder under it outside the temp
folder is refused with exit 2 before anything there is opened, so keep that
configuration in a copy, such as one under mktemp -d. A folder inside the temp
folder always drives, even when HOME points at it. Either way the
run prints the settings it resolved before the first step, so a pasted
transcript carries the conditions it was taken under.

Steps:
  type TEXT    type TEXT into the composer, leading spaces included
  paste TEXT   paste TEXT the way a terminal's own bracketed paste delivers it
  key NAME     press enter, esc, tab, space, backspace, up, down, left, right,
               any single character, or one of those under ctrl+, alt+ or shift+
               (alt+2 opens sub-agents, alt+4 opens shells, as in the app)
  click X Y [alt] [shift] [ctrl]         press and release the left button on a cell
  drag X1 Y1 X2 Y2 [alt] [shift] [ctrl]  press, move cell by cell, then release
  wheel X Y up|down [N]                  turn the wheel N notches over a cell, 1 if no N
  resize W H                             resize the terminal to W columns and H rows
  wait TEXT    wait until TEXT is on the screen, and fail saying so if it never is
  idle         wait until no turn runs and no sub-agent works, and fail naming
               the turn and the callers still asking the cassette if that never comes
  absent TEXT  fail if TEXT is on the screen now
  count TEXT   print how many rows of the screen hold TEXT, so a run proves a
               message shows once
  screen       print the screen as it stands
  environment  print the environment block the last turn sent to the model
  images       print how many images the last request's user message carried
  requests     print every request sent so far: its bytes, messages and tools as
               json, its tool count, a hash of its tools, and its tool_choice
               when that is not auto, then one line per message: its role, the
               bytes of its text, the images it carries, the id of each tool
               call it makes and of the call it answers
  requests text [N]  the same, with each message's text and tool calls under
               it, each line cut at --width and every key masked as the session
               log masks it; N prints only the Nth request sent
  # NOTE      a note, skipped

X and Y are zero-based cells: X counts columns and Y counts rows of the printed
screen, both from 0 at its top left.

The model is a cassette, one recorded reply per line of json. Its text arrives
the way a model's does, in deltas, so a reply is half written until it returns:
  {"text":"reading it","tools":[{"name":"read","args":{"path":"note.txt"}}]}
  {"text":"the note says a note"}
  {"text":"half an answer","unfinished":true}
  {"thinking":"the note is short","text":"it says a note"}

A reply's text can arrive in the deltas it names instead, joined into its text:
  {"deltas":["the answer arrives in ","two parts"]}

A reply's thinking streams before its text and shows only on the sub-agents screen.

A reply can report the usage a provider would, in tokens, the way anthropic
counts it: input excludes the cache. The fork decides on the larger of that
report, plus what came after it, and tofu's own estimate:
  {"text":"reading it","usage":{"input":2,"cache_read":150000,"cache_write":900}}

An unfinished reply streams its text and keeps writing until the turn is stopped,
which is how a driven run reaches an answer interrupted in the middle of itself.
holdAfterStop keeps it open that long after the stop, as a wire that ignores a
cancel does, so a driven run reaches a stop that does not end:
  {"agent":"c1","text":"still going","unfinished":true,"holdAfterStop":"20s"}

A refused reply is the error a wire returns for that HTTP status and body, so a
driven run reaches a context overflow or any other refusal without a network:
  {"refused":{"status":400,"detail":"prompt is too long: 213462 tokens > 200000 maximum"}}

Naming the wire reads the body and headers the way that wire does, and naming
the account says whose it was, so the chat words the refusal as it would live:
  {"refused":{"status":402,"wire":"codex","detail":"{\"error\":{\"code\":\"billing_not_configured\"}}",
   "account":{"source":"codex-sub","label":"#1","plan":"free"}}}
  {"refused":{"status":429,"wire":"anthropic","headers":{"retry-after":"3600"},"detail":"..."}}

A reply with no agent is the orchestrator's. A spawned sub-agent takes only the
replies addressed to it, so the two conversations never take each other's:
  {"agent":"c1","text":"reading it","tools":[{"name":"read","args":{"path":"x"}}]}

c1 is the first sub-agent, c2 the second, counted in the order they first ask,
which is the order the orchestrator spawns them. The first conversation is the
orchestrator's, and a later one is a sub-agent when it carries a spawn brief.
Every other conversation is a lead and takes the orchestrator's replies,
including one /new or /resume opens and one a cron fire or a hook opens. A
later conversation whose user messages carry no origin at all, which only a
hand-built request has, counts as a sub-agent.

A recorded reply is in flight for a moment before its first delta, so a turn
stopped while it is in flight draws nothing at all, as it would on a live wire.

TOFU_DRIVE_CASSETTE names the cassette when --cassette does not. Without one no
wire opens at all and a turn fails saying so, so a driven run reaches no network.

TOFU_DRIVE_CLOCK starts the session's clock at a local time, such as
2026-10-17T23:59:58, and runs it on from there, so a driven run crosses midnight
in seconds. It moves the date the model is told and the cron book, and it is
read only beside a cassette, here and in the tofu host and the server a cassette
run starts.

A turn that fails before its wire opens never says cooked for, so wait cooked for
sits until --timeout. Wait for the failure text instead, or for opens no live wire.
`

const (
	cassetteVariable   = "TOFU_DRIVE_CASSETTE"
	clockVariable      = "TOFU_DRIVE_CLOCK"
	requestsVariable   = "TOFU_DRIVE_REQUESTS"
	cassetteBuild      = "cassette"
	envOpen            = "<env>"
	envClose           = "</env>"
	stdinScript        = "-"
	driveHomePrefix    = "tofu-drive-home"
	noEnvironment      = "no turn has sent an environment block yet"
	noRequest          = "no turn has sent a request yet"
	recordedFlight     = konst.DriveSettleMillis * time.Millisecond
	orchestratorCaller = ""
	subAgentCaller     = "c"
)

type cassetteReply struct {
	Text          string   `json:"text"`
	Deltas        []string `json:"deltas"`
	Thinking      string   `json:"thinking"`
	Agent         string   `json:"agent"`
	Unfinished    bool     `json:"unfinished"`
	HoldAfterStop string   `json:"holdAfterStop"`
	Refused       *struct {
		Status  int               `json:"status"`
		Detail  string            `json:"detail"`
		Wire    string            `json:"wire"`
		Headers map[string]string `json:"headers"`
		Account transport.Account `json:"account"`
	} `json:"refused"`
	Tools []struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"tools"`
	Usage struct {
		Input      int `json:"input"`
		CacheRead  int `json:"cache_read"`
		CacheWrite int `json:"cache_write"`
	} `json:"usage"`
}

type recordedReply struct {
	decision      llm.Decision
	deltas        []string
	thinking      string
	unfinished    bool
	holdAfterStop time.Duration
	refused       error
	caller        string
}

type cassette struct {
	name          string
	decks         map[string][]recordedReply
	mutex         sync.Mutex
	taken         map[string]int
	asking        map[string]int
	callers       map[string]string
	subAgents     int
	firstOnSystem map[string]string
	last          llm.Request
	sent          []sentRequest
	dump          string
}

type sentRequest struct {
	summary  string
	messages []llm.Message
}

type requestsForm struct {
	text bool
	only int
}

func readRequestsForm(text string) (requestsForm, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return requestsForm{}, nil
	}
	form := requestsForm{text: fields[0] == "text"}
	if len(fields) == 2 {
		form.only, _ = strconv.Atoi(fields[1])
	}
	if !form.text || len(fields) > 2 || len(fields) == 2 && form.only < 1 {
		return requestsForm{}, fmt.Errorf("requests takes nothing, text, or text N, and was given %q", text)
	}
	return form, nil
}

func callerName(name string) string {
	if name == orchestratorCaller {
		return "the orchestrator"
	}
	return "sub-agent " + name
}

func subAgentNumber(agent string) bool {
	number, err := strconv.Atoi(strings.TrimPrefix(agent, subAgentCaller))
	return strings.HasPrefix(agent, subAgentCaller) && err == nil && number > 0
}

func driveClock(deck *cassette) (func() time.Time, error) {
	from := os.Getenv(clockVariable)
	if deck == nil || from == "" {
		return nil, nil
	}
	start, err := time.ParseInLocation(time.DateOnly+"T"+time.TimeOnly, from, time.Local)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a local time such as 2026-10-17T23:59:58", clockVariable, from)
	}
	began := time.Now()
	return func() time.Time { return start.Add(time.Since(began)) }, nil
}

func readCassette(path string) (*cassette, error) {
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	deck := &cassette{
		name:          filepath.Base(path),
		decks:         map[string][]recordedReply{},
		taken:         map[string]int{},
		asking:        map[string]int{},
		callers:       map[string]string{},
		firstOnSystem: map[string]string{},
		dump:          os.Getenv(requestsVariable),
	}
	for number, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var reply cassetteReply
		if err := json.Unmarshal([]byte(trimmed), &reply); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, number+1, err)
		}
		if reply.Agent != orchestratorCaller && !subAgentNumber(reply.Agent) {
			return nil, fmt.Errorf("%s line %d: agent %q is none of c1, c2 and so on, counting the callers after the orchestrator in the order they first ask", path, number+1, reply.Agent)
		}
		if len(reply.Deltas) == 0 && reply.Text != "" {
			reply.Deltas = []string{reply.Text}
		}
		decision := llm.Decision{Build: cassetteBuild, Outcome: llm.OutcomeMessage, Content: strings.Join(reply.Deltas, ""), FirstTokenMS: recordedFlight.Milliseconds(),
			Usage: llm.Usage{InputTokens: reply.Usage.Input}, PromptAccounting: llm.PromptExcludesCacheReads,
			CacheReadTokens: reply.Usage.CacheRead, CacheWriteTokens: reply.Usage.CacheWrite, Thinking: llm.Thinking{Text: reply.Thinking}}
		for index, one := range reply.Tools {
			decision.Outcome = llm.OutcomeToolCalls
			decision.ToolCalls = append(decision.ToolCalls, llm.ToolCall{
				ID:        fmt.Sprintf("%s_%d_%d", cassetteBuild, number+1, index+1),
				Name:      one.Name,
				Arguments: one.Args,
			})
		}
		recorded := recordedReply{decision: decision, deltas: reply.Deltas, thinking: reply.Thinking, unfinished: reply.Unfinished}
		if reply.HoldAfterStop != "" {
			if !reply.Unfinished {
				return nil, fmt.Errorf("%s line %d: holdAfterStop holds an unfinished reply open after its stop, and this reply finishes", path, number+1)
			}
			if recorded.holdAfterStop, err = time.ParseDuration(reply.HoldAfterStop); err != nil {
				return nil, fmt.Errorf("%s line %d: holdAfterStop: %w", path, number+1, err)
			}
		}
		if refused := reply.Refused; refused != nil {
			header := http.Header{}
			for name, value := range refused.Headers {
				header.Set(name, value)
			}
			var said *transport.Error
			switch refused.Wire {
			case "":
				said = &transport.Error{Kind: transport.StatusKind(refused.Status), Op: "cassette.Ask", Status: refused.Status, Detail: refused.Detail}
			case codex.Name:
				said = codex.Refusal(refused.Status, header, []byte(refused.Detail))
			case anthropic.Name:
				said = anthropic.Refusal(refused.Status, header, refused.Detail)
			default:
				return nil, fmt.Errorf("%s line %d: refused wire %q is none of %s and %s", path, number+1, refused.Wire, codex.Name, anthropic.Name)
			}
			said.Account = refused.Account
			if said.Account.Source != "" {
				said.Account.SignIn = loginHint(said.Account.Source)
			}
			recorded.refused = said
		}
		deck.decks[reply.Agent] = append(deck.decks[reply.Agent], recorded)
	}
	if len(deck.decks) == 0 {
		return nil, fmt.Errorf("%s holds no reply", path)
	}
	return deck, nil
}

func anthropicRefuses(request llm.Request) error {
	var system []string
	messages := make([]llm.Message, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Role == llm.RoleSystem {
			system = append(system, message.Content)
			continue
		}
		messages = append(messages, message)
	}
	_, err := anthropic.Request{Model: cassetteBuild, System: system, Messages: messages, Tools: request.Tools, ToolChoice: request.ToolChoice}.Encode(true)
	return err
}

func (c *cassette) take(request llm.Request) (recordedReply, error) {
	if err := anthropicRefuses(request); err != nil {
		return recordedReply{}, fmt.Errorf("%s: the anthropic wire would refuse this request: %w", c.name, err)
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.last = request
	system, opening, forked, briefed, marked := "", llm.Message{}, false, false, false
	for at, message := range request.Messages {
		if message.Role == llm.RoleSystem && system == "" {
			system = message.Content
		}
		briefed = briefed || message.Origin.Source == llm.SourceBrief
		marked = marked || message.Role == llm.RoleUser && message.Origin.Source != ""
		if message.Role == llm.RoleUser && message.Origin.Source != llm.SourceMemoryView && message.Origin.Source != llm.SourceEpisodes && opening.Role == llm.RoleUnknown {
			opening, forked = message, at+1 < len(request.Messages) && request.Messages[at+1].Origin.Source == llm.SourceForkCarry
		}
	}
	conversation := opening.Content
	if _, known := c.callers[conversation]; !known && forked {
		conversation = cmp.Or(c.firstOnSystem[system], conversation)
	}
	name, known := c.callers[conversation]
	if !known {
		name = orchestratorCaller
		if len(c.callers) > 0 && (briefed || !marked) {
			c.subAgents++
			name = subAgentCaller + strconv.Itoa(c.subAgents)
		}
		c.callers[conversation] = name
		c.firstOnSystem[system] = cmp.Or(c.firstOnSystem[system], conversation)
	}
	if c.taken[name] >= len(c.decks[name]) {
		return recordedReply{}, fmt.Errorf("%s holds %d replies for %s and %s asked for one more", c.name, len(c.decks[name]), callerName(name), callerName(name))
	}
	c.taken[name]++
	body, err := json.Marshal(struct {
		Messages []llm.Message
		Tools    []llm.Tool
	}{request.Messages, request.Tools})
	if err != nil {
		return recordedReply{}, err
	}
	tools, err := json.Marshal(request.Tools)
	if err != nil {
		return recordedReply{}, err
	}
	line := fmt.Sprintf("%s request %d: %d bytes, %d tools %s", callerName(name), c.taken[name], len(body), len(request.Tools), digest(string(tools)))
	if choice := request.ToolChoice.OpenAIValue(); choice != "" {
		line += ", tool_choice " + choice
	}
	sent := sentRequest{summary: line, messages: slices.Clone(request.Messages)}
	for at := range sent.messages {
		sent.messages[at].Images = make([]llm.Image, len(sent.messages[at].Images))
	}
	c.sent = append(c.sent, sent)
	if c.dump != "" {
		if err := appendLines(c.dump, sent.lines(requestsForm{}, 0, sys.KeyRedactor{})); err != nil {
			return recordedReply{}, fmt.Errorf("%s: %w", requestsVariable, err)
		}
	}
	c.asking[name]++
	reply := c.decks[name][c.taken[name]-1]
	reply.caller = name
	return reply, nil
}

func (c *cassette) requests(form requestsForm, width int) string {
	if c == nil {
		return noRequest
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if form.only > len(c.sent) {
		return fmt.Sprintf("%d requests were sent, so there is no request %d", len(c.sent), form.only)
	}
	redactor := sys.LoadKeyRedactor()
	var lines []string
	for number, sent := range c.sent {
		if form.only == 0 || form.only == number+1 {
			lines = append(lines, sent.lines(form, width, redactor)...)
		}
	}
	return cmp.Or(strings.Join(lines, "\n"), noRequest)
}

func (sent sentRequest) lines(form requestsForm, width int, redactor sys.KeyRedactor) []string {
	lines := []string{sent.summary}
	for index, message := range sent.messages {
		text := message.Sent()
		line := fmt.Sprintf("  message %d %s: %d bytes", index+1, message.Role, len(text))
		if len(message.Images) > 0 {
			line += fmt.Sprintf(", %d image(s)", len(message.Images))
		}
		if message.ToolCallID != "" {
			line += ", result of " + message.ToolCallID
		}
		var said []string
		if text != "" {
			said = append(said, text)
		}
		for _, call := range message.ToolCalls {
			line += ", calls " + call.Name + " " + call.ID
			said = append(said, call.Name+" "+string(call.Arguments))
		}
		lines = append(lines, line)
		if !form.text {
			continue
		}
		for _, one := range strings.Split(redactor.Redact(strings.Join(said, "\n")), "\n") {
			lines = append(lines, widget.Fit("    "+one, width))
		}
	}
	return lines
}

func appendLines(path string, lines []string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(strings.Join(lines, "\n") + "\n")
	return errors.Join(err, file.Close())
}

func (c *cassette) Ask(ctx context.Context, request llm.Request) (llm.Decision, error) {
	select {
	case <-ctx.Done():
		return llm.Decision{}, ctx.Err()
	case <-time.After(recordedFlight):
	}
	reply, err := c.take(request)
	if err != nil {
		return llm.Decision{}, err
	}
	defer func() {
		c.mutex.Lock()
		c.asking[reply.caller]--
		c.mutex.Unlock()
	}()
	if reply.refused != nil {
		return llm.Decision{}, reply.refused
	}
	if request.OnThinking != nil && reply.thinking != "" {
		request.OnThinking(reply.thinking)
	}
	for _, delta := range reply.deltas {
		if request.OnDelta != nil {
			request.OnDelta(delta)
		}
	}
	if !reply.unfinished {
		return reply.decision, nil
	}
	<-ctx.Done()
	time.Sleep(reply.holdAfterStop)
	return llm.Decision{}, ctx.Err()
}

func (c *cassette) askers() string {
	if c == nil {
		return ""
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	var asking []string
	for name, count := range c.asking {
		if count > 0 {
			asking = append(asking, callerName(name))
		}
	}
	if len(asking) == 0 {
		return ", between requests to the cassette"
	}
	slices.Sort(asking)
	return ", with " + strings.Join(asking, " and ") + " still waiting on a recorded reply"
}

func awaitIdle(driver *filmstrip.Driver, turnRunning func() (string, bool), deck *cassette, within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		if err := driver.Settle(); err != nil {
			return err
		}
		id, running := turnRunning()
		if !running {
			return driver.Settle()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waited %s for no turn to run, and turn %s still runs%s", within, id, deck.askers())
		}
	}
}

func (c *cassette) environment() string {
	if c == nil {
		return noEnvironment
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, message := range slices.Backward(c.last.Messages) {
		open := strings.Index(message.Content, envOpen)
		closing := strings.Index(message.Content, envClose)
		if open >= 0 && closing > open {
			return message.Content[open : closing+len(envClose)]
		}
	}
	return noEnvironment
}

func (c *cassette) images() string {
	if c == nil {
		return noRequest
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for index := len(c.last.Messages) - 1; index >= 0; index-- {
		if message := c.last.Messages[index]; message.Role == llm.RoleUser {
			return "the last user message carried " + strconv.Itoa(len(message.Images)) + " image(s)"
		}
	}
	return noRequest
}

func driveWire(deck *cassette) func(runOpts) (appWire, error) {
	return func(opts runOpts) (appWire, error) {
		if deck == nil {
			return appWire{}, errors.New("tofu drive opens no live wire: name a recorded one with --cassette or " + cassetteVariable)
		}
		standingIn := models.Model{ID: cassetteBuild, Windows: []string{cassetteBuild}}
		if chosen, err := chooseModel(opts); err == nil {
			standingIn.Subscription, standingIn.Vision, standingIn.PromptCache = chosen.Subscription, chosen.Vision, chosen.PromptCache
		}
		return appWire{
			held:     &accounts{fixed: deck, now: time.Now},
			spend:    turn.SpendSubscription,
			selected: standingIn,
		}, nil
	}
}

type quotaReading struct {
	frame.Quota
	ResetsIn string `json:"resetsIn"`
}

func readQuotas(path string, held heldStore) (func() []frame.Quota, error) {
	if path == "" {
		return held.quota, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var readings []quotaReading
	if err := json.Unmarshal(body, &readings); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	resetsIn := make([]time.Duration, len(readings))
	for index, reading := range readings {
		if reading.ResetsIn == "" {
			continue
		}
		if resetsIn[index], err = time.ParseDuration(reading.ResetsIn); err != nil {
			return nil, fmt.Errorf("%s reading %d: %w", path, index+1, err)
		}
	}
	return func() []frame.Quota {
		now := time.Now()
		quotas := make([]frame.Quota, len(readings))
		for index, reading := range readings {
			quotas[index] = reading.Quota
			if resetsIn[index] > 0 {
				quotas[index].ResetsAt = now.Add(resetsIn[index])
			}
		}
		return quotas
	}, nil
}

func drivenApp(dir string, deck *cassette, clock func() time.Time, plan drivePlan, launch appLaunch, held heldStore, quotas func() []frame.Quota) *tui.App {
	shown := cassetteBuild
	if deck != nil {
		shown = deck.name
	}
	recorded := appWiring{
		open: driveWire(deck),
		wires: func() []tui.Wire {
			if plan.setup {
				return held.wires()
			}
			library, _ := modelLibrary(dir)
			listed := []tui.Wire{{Name: library.WireFor(models.Subscription(plan.source)), Model: shown, Provider: cmp.Or(plan.source, cassetteBuild)}}
			if plan.quota == "" {
				return listed
			}
			for _, spec := range library.Subscriptions {
				source := string(spec.ID)
				named := slices.ContainsFunc(quotas(), func(reading frame.Quota) bool { return readingSource(reading) == source })
				if named && source != listed[0].Provider {
					listed = append(listed, tui.Wire{Name: spec.Wire, Model: shown, Provider: source})
				}
			}
			return listed
		},
		blockers: func() []tui.Requirement {
			if plan.setup {
				return held.requirements()
			}
			return nil
		},
		clipboard: func() (sys.Clipboard, error) {
			if plan.clipboard == "" {
				return sys.Clipboard{Kind: sys.ClipboardEmpty}, nil
			}
			return sys.Clipboard{Kind: sys.ClipboardFiles, Files: []string{plan.clipboard}}, nil
		},
		quota: quotas,
		now:   clock,
		joins: plan.host,
	}
	return tui.New(appOptions(dir, plan.arms, recorded, launch, held))
}

func readScript(path string, in io.Reader) ([]filmstrip.Step, error) {
	if path != stdinScript {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		in = file
	}
	body, err := io.ReadAll(in)
	return filmstrip.ReadScript(string(body)), err
}

type drivePlan struct {
	script    string
	dir       string
	home      string
	cassette  string
	clipboard string
	source    string
	quota     string
	width     int
	height    int
	timeout   time.Duration
	plain     bool
	fresh     bool
	resume    bool
	host      bool
	setup     bool
	arms      runOpts
}

func armIsKnown(flag, taken string, arms []string) error {
	if taken == "" || slices.Contains(arms, taken) {
		return nil
	}
	return fmt.Errorf("%s %q is none of %s, and without it the app decides as it always does", flag, taken, strings.Join(arms, ", "))
}

func driveArgs(args []string, errOut io.Writer) (drivePlan, bool) {
	plan := drivePlan{
		script:   stdinScript,
		cassette: os.Getenv(cassetteVariable),
		width:    fixture.Width,
		height:   fixture.Height,
		timeout:  konst.DriveTimeoutMillis * time.Millisecond,
	}
	refuse := func(said string, err error) (drivePlan, bool) {
		_, _ = fmt.Fprintf(errOut, "tofu drive: %s%v\n\n%s", said, err, driveUsage)
		return drivePlan{}, false
	}
	for index := 0; index < len(args); index++ {
		flag := args[index]
		switch flag {
		case "--plain":
			plan.plain = true
			continue
		case "--fresh":
			plan.fresh = true
			continue
		case "--continue":
			plan.resume = true
			continue
		case "--host":
			plan.host = true
			continue
		case "--setup":
			plan.setup = true
			continue
		case "--no-gate":
			plan.arms.gateArm = gateOff
			continue
		case "--no-subagents":
			plan.arms.noSubAgents = true
			continue
		case "--no-instructions":
			plan.arms.noInstructions = true
			continue
		case "--no-docs":
			plan.arms.noDocs = true
			continue
		}
		if !strings.HasPrefix(flag, "--") {
			plan.script = flag
			continue
		}
		index++
		if index >= len(args) {
			return refuse("", fmt.Errorf("%s wants a value", flag))
		}
		taken := args[index]
		var err error
		switch flag {
		case "--dir":
			plan.dir = taken
		case "--home":
			plan.home = taken
		case "--cassette":
			plan.cassette = taken
		case "--clipboard":
			plan.clipboard = taken
		case "--source":
			plan.source = taken
		case "--quota":
			plan.quota = taken
		case "--width":
			plan.width, err = strconv.Atoi(taken)
		case "--height":
			plan.height, err = strconv.Atoi(taken)
		case "--timeout":
			plan.timeout, err = time.ParseDuration(taken)
		case "--gate":
			plan.arms.gateArm = taken
		case "--sift":
			plan.arms.siftArm = taken
		case "--done-review":
			plan.arms.doneArm = taken
		case "--tools":
			plan.arms.toolSet = taken
		case "--max-steps":
			plan.arms.maxSteps, err = strconv.Atoi(taken)
		case "--context-ceiling":
			plan.arms.contextCeiling, err = strconv.Atoi(taken)
		default:
			err = errors.New("no such flag")
		}
		if err != nil {
			return refuse(flag+" "+strconv.Quote(taken)+": ", err)
		}
	}
	if err := cmp.Or(
		armIsKnown("--gate", plan.arms.gateArm, gateArms()),
		armIsKnown("--sift", plan.arms.siftArm, siftArms()),
		armIsKnown("--done-review", plan.arms.doneArm, settingspkg.DoneReviews()),
		armIsKnown("--tools", plan.arms.toolSet, []string{toolSetFull, toolSetThree}),
		armIsKnown("--source", plan.source, []string{string(cred.ClaudeSub), string(cred.CodexSub)}),
	); err != nil {
		return refuse("", err)
	}
	for _, path := range []*string{&plan.script, &plan.home, &plan.cassette, &plan.quota, &plan.clipboard} {
		if *path == "" || *path == stdinScript {
			continue
		}
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return refuse(*path+": ", err)
		}
		*path = absolute
	}
	return plan, true
}

func driveHome(named string) (release func(), err error) {
	home := named
	if home == "" {
		if home, err = os.MkdirTemp("", driveHomePrefix); err != nil {
			return nil, err
		}
	} else if err = refuseTheRealHome(named); err != nil {
		return nil, err
	}
	previous := map[string]string{}
	for _, variable := range []string{"HOME", "USERPROFILE"} {
		previous[variable] = os.Getenv(variable)
		if err = os.Setenv(variable, home); err != nil {
			return nil, err
		}
	}
	return func() {
		for variable, value := range previous {
			_ = os.Setenv(variable, value)
		}
		if named == "" {
			_ = os.RemoveAll(home)
		}
	}, nil
}

func refuseTheRealHome(named string) error {
	real, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("--home %s cannot be checked against your real home, so the drive does not start: %w", named, err)
	}
	absolute := func(path string) string {
		abs, _ := filepath.Abs(path)
		return abs
	}
	linked := func(path string) string {
		abs := absolute(path)
		if target, err := filepath.EvalSymlinks(abs); err == nil {
			return target
		}
		return abs
	}
	within := func(parent, child string) bool {
		for _, spelled := range []func(string) string{absolute, linked} {
			inside, err := filepath.Rel(spelled(parent), spelled(child))
			if err == nil && inside != ".." && !strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	temp := os.TempDir()
	insideTemp := func(path string) bool { return within(temp, path) && !within(path, temp) }
	state := within(filepath.Join(real, sys.StateDirName), named) && !insideTemp(real)
	if state || !insideTemp(named) && (within(named, real) || within(real, named)) {
		return fmt.Errorf("--home %s is your real home %s, its %s, a folder above it, or a folder under it outside %s, and a drive never runs against it; name a folder made for the run, or leave --home out for a fresh one", named, real, sys.StateDirName, temp)
	}
	return nil
}

func printDriveSettings(out io.Writer, dir, named string) {
	where := "settings: no --home was named, so this run made an empty one and read no settings file of yours"
	if named != "" {
		where = "settings: read from the home named by --home " + named
	}
	_, _ = fmt.Fprintln(out, where)
	store, err := openSettings(dir)
	if err != nil {
		_, _ = fmt.Fprintf(out, "settings: unreadable, so every setting is its declared default: %v\n", err)
		return
	}
	printSettingsList(out, store, dir)
}

func driveFail(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "tofu drive: %v\n", err)
	return exitUsage
}

func driveVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	if slices.Contains(args, "--help") || slices.Contains(args, "-h") {
		_, _ = fmt.Fprint(out, driveUsage)
		return exitOK
	}
	plan, taken := driveArgs(args, errOut)
	if !taken {
		return exitUsage
	}
	if plan.dir != "" {
		if err := os.Chdir(plan.dir); err != nil {
			return driveFail(errOut, err)
		}
	}
	dir, err := os.Getwd()
	if err != nil {
		return driveFail(errOut, err)
	}
	release, err := driveHome(plan.home)
	if err != nil {
		return driveFail(errOut, err)
	}
	defer release()
	printDriveSettings(out, dir, plan.home)
	steps, err := readScript(plan.script, in)
	if err != nil {
		return driveFail(errOut, err)
	}
	deck, err := readCassette(plan.cassette)
	if err != nil {
		return driveFail(errOut, err)
	}
	if plan.host && deck != nil {
		_ = os.Setenv(cassetteVariable, plan.cassette)
	}
	if plan.host {
		_ = os.Setenv(doneReviewVariable, plan.arms.doneArm)
	}
	clock, err := driveClock(deck)
	if err != nil {
		return driveFail(errOut, err)
	}
	held := holdStore()
	defer held.close()
	quotas, err := readQuotas(plan.quota, held)
	if err != nil {
		return driveFail(errOut, err)
	}
	resumed := sessionResume{}
	store, err := sessionstore.Open()
	if err != nil {
		return driveFail(errOut, err)
	}
	if plan.resume {
		resumed = continueCarry(store)
	}
	defer printRecorded(out, store, eventSizes(store), dir, plan.home)
	launch := launchOf(dir, resumed, plan.fresh)
	defer leaveShells(launch.registry)
	app := drivenApp(dir, deck, clock, plan, launch, held, quotas)
	driver := filmstrip.Drive(app, plan.width, plan.height, crashDir(dir))
	defer driver.Close()
	defer (*launch.release)()
	defer func() {
		for _, warning := range (*launch.endSession)() {
			_, _ = fmt.Fprintln(errOut, "tofu drive: "+warning)
		}
	}()
	for _, step := range steps {
		err := playStep(driver, deck, *launch.turnRunning, step, plan, out)
		if err == nil {
			continue
		}
		if step.Verb == "wait" || step.Verb == "idle" || step.Verb == "absent" {
			_, _ = fmt.Fprintf(errOut, "tofu drive: line %d: %v\n\n%s\n", step.Line, err, driver.Plain())
			return exitVerdict
		}
		return driveFail(errOut, fmt.Errorf("line %d: %w", step.Line, err))
	}
	return exitOK
}

func eventSizes(store *sessionstore.Store) map[string]int64 {
	sizes := map[string]int64{}
	listing, _ := store.Listing()
	for _, header := range listing.Sessions {
		if info, err := os.Stat(store.EventsPath(header.ID)); err == nil {
			sizes[header.ID] = info.Size()
		}
	}
	return sizes
}

func printRecorded(out io.Writer, store *sessionstore.Store, before map[string]int64, dir, home string) {
	after := eventSizes(store)
	listing, _ := store.Listing()
	at := slices.IndexFunc(listing.Sessions, func(header sessionstore.Header) bool {
		size, written := after[header.ID]
		return written && size != before[header.ID]
	})
	if at < 0 {
		_, _ = fmt.Fprintln(out, "drive: no session was recorded")
		return
	}
	handle := listing.Sessions[at].ID
	if identity, err := store.Identity(handle); err == nil {
		handle = identity.Handle()
	}
	if home == "" {
		_, _ = fmt.Fprintf(out, "drive: recorded session %s in a home this run made and deletes, so name --home to read it afterwards\n", handle)
		return
	}
	quoted := func(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }
	variable := "HOME"
	if goruntime.GOOS == "windows" {
		variable = "USERPROFILE"
	}
	binary, err := os.Executable()
	if err != nil {
		binary = "tofu"
	}
	_, _ = fmt.Fprintf(out, "drive: recorded session %s; read it with: cd %s && %s=%s %s session trace %s\n", handle, quoted(dir), variable, quoted(home), quoted(binary), quoted(handle))
}

func playStep(driver *filmstrip.Driver, deck *cassette, turnRunning func() (string, bool), step filmstrip.Step, plan drivePlan, out io.Writer) error {
	said := deck.environment
	switch step.Verb {
	case "idle":
		if step.Text != "" {
			return fmt.Errorf("idle takes nothing, and was given %q", step.Text)
		}
		return awaitIdle(driver, turnRunning, deck, plan.timeout)
	case "count":
		if step.Text == "" {
			return errors.New("count takes the text to count, and was given none")
		}
		said = func() string {
			held := 0
			for row := range strings.SplitSeq(driver.Plain(), "\n") {
				if strings.Contains(row, step.Text) {
					held++
				}
			}
			return fmt.Sprintf("count %s: %d", step.Text, held)
		}
	case "environment":
	case "images":
		said = deck.images
	case "requests":
		form, err := readRequestsForm(step.Text)
		if err != nil {
			return err
		}
		said = func() string { return deck.requests(form, plan.width) }
	default:
		return driver.Play(step, plan.timeout, plan.plain, out)
	}
	if err := driver.Settle(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, said())
	return err
}
