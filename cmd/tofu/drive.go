package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	sessionstore "tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/transport"
	"tofu/internal/turn"
	"tofu/internal/widget"
)

const driveUsage = `usage: tofu drive [SCRIPT] [--dir PATH] [--home PATH] [--cassette PATH] [--clipboard PATH] [--source NAME] [--quota PATH] [--width N] [--height N] [--timeout 60s] [--plain] [--fresh | --continue] [ARM]

drives the app the way a person does, with no terminal and no model call.
SCRIPT is a file of steps, or - for standard input.

--source claude-sub|codex-sub names the subscription the orchestrator's model
reports, while the cassette still answers. Without it the source is cassette.

--quota PATH reads the footer's quota readings from a json file instead of
asking the vendors, which a fresh home cannot do:
  [{"label":"claude-sub 5h","fraction":0.62,"reported":true,"resetsIn":"3h28m"}]

The driven app never reads your clipboard. --clipboard PATH makes it hold that
file, as a file copied in the explorer does, so ctrl+v attaches it; without it
the clipboard is empty.

--fresh starts the app as a new session does: the chat, with the tofu cat and
wordmark in the empty transcript until the first message. --continue starts it
on the session you last worked in, as tofu --continue does. With neither the
app opens on an empty chat without the art.

An arm is one of tofu run's switches that change what the turn does rather than
where it reads from, and the app takes it exactly as tofu run does, so a
mechanism can be driven by hand on a cassette rather than argued about:
  --sift free|judged        which arm cuts a bash result before the model reads
                            it, otherwise the method table decides
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
which is how you drive the app against a configuration you keep. Either way the
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
  absent TEXT  fail if TEXT is on the screen now
  screen       print the screen as it stands
  environment  print the environment block the last turn sent to the model
  images       print how many images the last request's user message carried
  requests     print every request sent so far: its bytes, messages and tools as
               json, its tool count, a hash of its tools, and its tool_choice
               when that is not auto, then one line per message: its role, the
               bytes of its text, the id of each tool call it makes and of the
               call it answers
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

A reply's thinking streams before its text and shows only on the sub-agents screen.

A reply can report the usage a provider would, in tokens, the way anthropic
counts it: input excludes the cache. The fork decides on the larger of that
report, plus what came after it, and tofu's own estimate:
  {"text":"reading it","usage":{"input":2,"cache_read":150000,"cache_write":900}}

An unfinished reply streams its text and keeps writing until the turn is stopped,
which is how a driven run reaches an answer interrupted in the middle of itself.

A refused reply is the error a wire returns for that HTTP status and body, so a
driven run reaches a context overflow or any other refusal without a network:
  {"refused":{"status":400,"detail":"prompt is too long: 213462 tokens > 200000 maximum"}}

A reply with no agent is the orchestrator's. A spawned sub-agent takes only the
replies addressed to it, so the two conversations never take each other's:
  {"agent":"c1","text":"reading it","tools":[{"name":"read","args":{"path":"x"}}]}

c1 is the first caller after the orchestrator, c2 the second, counted in the order
they first ask, which is the order the orchestrator spawns them.

A recorded reply is in flight for a moment before its first delta, so a turn
stopped while it is in flight draws nothing at all, as it would on a live wire.

TOFU_DRIVE_CASSETTE names the cassette when --cassette does not. Without one no
wire opens at all and a turn fails saying so, so a driven run reaches no network.

A turn that fails before its wire opens never says cooked for, so wait cooked for
sits until --timeout. Wait for the failure text instead, or for opens no live wire.
`

const (
	cassetteVariable   = "TOFU_DRIVE_CASSETTE"
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
	Text       string `json:"text"`
	Thinking   string `json:"thinking"`
	Agent      string `json:"agent"`
	Unfinished bool   `json:"unfinished"`
	Refused    *struct {
		Status int    `json:"status"`
		Detail string `json:"detail"`
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
	decision   llm.Decision
	thinking   string
	unfinished bool
	refused    error
}

type cassette struct {
	name    string
	decks   map[string][]recordedReply
	mutex   sync.Mutex
	taken   map[string]int
	callers map[string]string
	last    llm.Request
	sent    []sentRequest
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

func readCassette(path string) (*cassette, error) {
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	deck := &cassette{
		name:    filepath.Base(path),
		decks:   map[string][]recordedReply{},
		taken:   map[string]int{},
		callers: map[string]string{},
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
		decision := llm.Decision{Build: cassetteBuild, Outcome: llm.OutcomeMessage, Content: reply.Text, FirstTokenMS: recordedFlight.Milliseconds(),
			Usage: llm.Usage{InputTokens: reply.Usage.Input}, PromptAccounting: llm.PromptExcludesCacheReads,
			CacheReadTokens: reply.Usage.CacheRead, CacheWriteTokens: reply.Usage.CacheWrite}
		for index, one := range reply.Tools {
			decision.Outcome = llm.OutcomeToolCalls
			decision.ToolCalls = append(decision.ToolCalls, llm.ToolCall{
				ID:        fmt.Sprintf("%s_%d_%d", cassetteBuild, number+1, index+1),
				Name:      one.Name,
				Arguments: one.Args,
			})
		}
		recorded := recordedReply{decision: decision, thinking: reply.Thinking, unfinished: reply.Unfinished}
		if refused := reply.Refused; refused != nil {
			recorded.refused = &transport.Error{Kind: transport.StatusKind(refused.Status), Op: "cassette.Ask", Status: refused.Status, Detail: refused.Detail}
		}
		deck.decks[reply.Agent] = append(deck.decks[reply.Agent], recorded)
	}
	if len(deck.decks) == 0 {
		return nil, fmt.Errorf("%s holds no reply", path)
	}
	return deck, nil
}

func (c *cassette) take(request llm.Request) (recordedReply, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.last = request
	conversation := ""
	for _, message := range request.Messages {
		if message.Role == llm.RoleUser {
			conversation = message.Content
			break
		}
	}
	name, known := c.callers[conversation]
	if !known {
		name = orchestratorCaller
		if len(c.callers) > 0 {
			name = subAgentCaller + strconv.Itoa(len(c.callers))
		}
		c.callers[conversation] = name
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
	c.sent = append(c.sent, sentRequest{summary: line, messages: slices.Clone(request.Messages)})
	return c.decks[name][c.taken[name]-1], nil
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
		if form.only != 0 && form.only != number+1 {
			continue
		}
		lines = append(lines, sent.summary)
		for index, message := range sent.messages {
			line := fmt.Sprintf("  message %d %s: %d bytes", index+1, message.Role, len(message.Content))
			if message.ToolCallID != "" {
				line += ", result of " + message.ToolCallID
			}
			var said []string
			if message.Content != "" {
				said = append(said, message.Content)
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
	}
	return cmp.Or(strings.Join(lines, "\n"), noRequest)
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
	if reply.refused != nil {
		return llm.Decision{}, reply.refused
	}
	if request.OnThinking != nil && reply.thinking != "" {
		request.OnThinking(reply.thinking)
	}
	if request.OnDelta != nil && reply.decision.Content != "" {
		request.OnDelta(reply.decision.Content)
	}
	if !reply.unfinished {
		return reply.decision, nil
	}
	<-ctx.Done()
	return llm.Decision{}, ctx.Err()
}

func (c *cassette) environment() string {
	if c == nil {
		return noEnvironment
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	for _, message := range c.last.Messages {
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
			standingIn.Subscription, standingIn.Vision = chosen.Subscription, chosen.Vision
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

func readQuotas(path string) (func() []frame.Quota, error) {
	if path == "" {
		return appQuota, nil
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

func drivenApp(dir string, deck *cassette, plan drivePlan, launch appLaunch, quotas func() []frame.Quota) *tui.App {
	shown := cassetteBuild
	if deck != nil {
		shown = deck.name
	}
	recorded := appWiring{
		open: driveWire(deck),
		wires: func() []tui.Wire {
			return []tui.Wire{{Name: wireSubscription, Model: shown, Provider: cmp.Or(plan.source, cassetteBuild)}}
		},
		blockers: func() []tui.Requirement { return nil },
		clipboard: func() (sys.Clipboard, error) {
			if plan.clipboard == "" {
				return sys.Clipboard{Kind: sys.ClipboardEmpty}, nil
			}
			return sys.Clipboard{Kind: sys.ClipboardFiles, Files: []string{plan.clipboard}}, nil
		},
		quota: quotas,
	}
	return tui.New(appOptions(dir, plan.arms, recorded, launch))
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
		armIsKnown("--tools", plan.arms.toolSet, []string{toolSetFull, toolSetThree}),
		armIsKnown("--source", plan.source, []string{string(cred.ClaudeSub), string(cred.CodexSub)}),
	); err != nil {
		return refuse("", err)
	}
	return plan, true
}

func driveHome(named string) (release func(), err error) {
	home := named
	if home == "" {
		if home, err = os.MkdirTemp("", driveHomePrefix); err != nil {
			return nil, err
		}
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
	printSettingsList(out, store)
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
	quotas, err := readQuotas(plan.quota)
	if err != nil {
		return driveFail(errOut, err)
	}
	resumed := sessionResume{}
	if store, err := sessionstore.Open(); err == nil && plan.resume {
		resumed = continueCarry(store)
	}
	launch := launchOf(dir, resumed, plan.fresh)
	defer leaveShells(launch.registry)
	driver := filmstrip.Drive(drivenApp(dir, deck, plan, launch, quotas), plan.width, plan.height)
	defer driver.Close()
	for _, step := range steps {
		err := playStep(driver, deck, step, plan, out)
		if err == nil {
			continue
		}
		if step.Verb == "wait" || step.Verb == "absent" {
			_, _ = fmt.Fprintf(errOut, "tofu drive: line %d: %v\n\n%s\n", step.Line, err, driver.Plain())
			return exitVerdict
		}
		return driveFail(errOut, fmt.Errorf("line %d: %w", step.Line, err))
	}
	return exitOK
}

func playStep(driver *filmstrip.Driver, deck *cassette, step filmstrip.Step, plan drivePlan, out io.Writer) error {
	said := deck.environment
	switch step.Verb {
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
