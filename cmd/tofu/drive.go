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
	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/turn"
)

const driveUsage = `usage: tofu drive [SCRIPT] [--dir PATH] [--home PATH] [--cassette PATH] [--width N] [--height N] [--timeout 60s] [--plain] [ARM]

drives the app the way a person does, with no terminal and no model call.
SCRIPT is a file of steps, or - for standard input.

An arm is one of tofu run's switches that change what the turn does rather than
where it reads from, and the app takes it exactly as tofu run does, so a
mechanism can be driven by hand on a cassette rather than argued about:
  --sift free|judged        which arm cuts a bash result before the model reads
                            it, otherwise the method table decides
  --gate off|shadow|enforce, --no-gate    how a tool call is judged
  --tools full|three        which tools the turn is given
  --no-subagents            run without the spawn tool
  --no-instructions         send no AGENTS.md or CLAUDE.md
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
  type TEXT    type TEXT into the composer
  key NAME     press enter, esc, tab, space, backspace, up, down, left, right,
               any single character, or one of those under ctrl+, alt+ or shift+
               (alt+2 opens work, alt+5 opens shells, as in the app)
  click X Y [alt] [shift] [ctrl]         press and release the left button on a cell
  drag X1 Y1 X2 Y2 [alt] [shift] [ctrl]  press, move cell by cell, then release
  wheel X Y up|down [N]                  turn the wheel N notches over a cell, 1 if no N
  resize W H                             resize the terminal to W columns and H rows
  wait TEXT    wait until TEXT is on the screen, and fail saying so if it never is
  screen       print the screen as it stands
  environment  print the environment block the last turn sent to the model
  # NOTE       a note, skipped

X and Y are zero-based cells: X counts columns and Y counts rows of the printed
screen, both from 0 at its top left.

The model is a cassette, one recorded reply per line of json. Its text arrives
the way a model's does, in deltas, so a reply is half written until it returns:
  {"text":"reading it","tools":[{"name":"read","args":{"path":"note.txt"}}]}
  {"text":"the note says a note"}
  {"text":"half an answer","unfinished":true}

An unfinished reply streams its text and keeps writing until the turn is stopped,
which is how a driven run reaches an answer interrupted in the middle of itself.

A reply with no agent is the parent's. A spawned child takes only the replies
addressed to it, so the two conversations never take each other's:
  {"agent":"c1","text":"reading it","tools":[{"name":"read","args":{"path":"x"}}]}

c1 is the first caller after the parent, c2 the second, counted in the order they
first ask, which is the order the parent spawns them.

A recorded reply is in flight for a moment before its first delta, so a turn
stopped while it is in flight draws nothing at all, as it would on a live wire.

TOFU_DRIVE_CASSETTE names the cassette when --cassette does not. Without one no
wire opens at all and a turn fails saying so, so a driven run reaches no network.

A turn that fails before its wire opens never says cooked for, so wait cooked for
sits until --timeout. Wait for the failure text instead, or for opens no live wire.
`

const (
	cassetteVariable = "TOFU_DRIVE_CASSETTE"
	cassetteBuild    = "cassette"
	envOpen          = "<env>"
	envClose         = "</env>"
	stdinScript      = "-"
	driveHomePrefix  = "tofu-drive-home"
	noEnvironment    = "no turn has sent an environment block yet"
	recordedFlight   = konst.DriveSettleMillis * time.Millisecond
	parentCaller     = ""
	childCaller      = "c"
)

type cassetteReply struct {
	Text       string `json:"text"`
	Agent      string `json:"agent"`
	Unfinished bool   `json:"unfinished"`
	Tools      []struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"tools"`
}

type recordedReply struct {
	decision   llm.Decision
	unfinished bool
}

type cassette struct {
	name    string
	decks   map[string][]recordedReply
	mutex   sync.Mutex
	taken   map[string]int
	callers map[string]string
	last    llm.Request
}

func callerName(name string) string {
	if name == parentCaller {
		return "the parent"
	}
	return "child " + name
}

func childNumber(agent string) bool {
	number, err := strconv.Atoi(strings.TrimPrefix(agent, childCaller))
	return strings.HasPrefix(agent, childCaller) && err == nil && number > 0
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
		if reply.Agent != parentCaller && !childNumber(reply.Agent) {
			return nil, fmt.Errorf("%s line %d: agent %q is none of c1, c2 and so on, counting the callers after the parent in the order they first ask", path, number+1, reply.Agent)
		}
		decision := llm.Decision{Build: cassetteBuild, Outcome: llm.OutcomeMessage, Content: reply.Text}
		for index, one := range reply.Tools {
			decision.Outcome = llm.OutcomeToolCalls
			decision.ToolCalls = append(decision.ToolCalls, llm.ToolCall{
				ID:        fmt.Sprintf("%s_%d_%d", cassetteBuild, number+1, index+1),
				Name:      one.Name,
				Arguments: one.Args,
			})
		}
		deck.decks[reply.Agent] = append(deck.decks[reply.Agent], recordedReply{decision: decision, unfinished: reply.Unfinished})
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
		name = parentCaller
		if len(c.callers) > 0 {
			name = childCaller + strconv.Itoa(len(c.callers))
		}
		c.callers[conversation] = name
	}
	if c.taken[name] >= len(c.decks[name]) {
		return recordedReply{}, fmt.Errorf("%s holds %d replies for %s and %s asked for one more", c.name, len(c.decks[name]), callerName(name), callerName(name))
	}
	c.taken[name]++
	return c.decks[name][c.taken[name]-1], nil
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

func driveWire(deck *cassette) func(runOpts) (appWire, error) {
	return func(runOpts) (appWire, error) {
		if deck == nil {
			return appWire{}, errors.New("tofu drive opens no live wire: name a recorded one with --cassette or " + cassetteVariable)
		}
		return appWire{
			held:     &accounts{fixed: deck, now: time.Now},
			spend:    turn.SpendSubscription,
			selected: models.Model{ID: cassetteBuild, Windows: []string{cassetteBuild}},
		}, nil
	}
}

func drivenApp(dir string, deck *cassette, arms runOpts) *tui.App {
	shown := cassetteBuild
	if deck != nil {
		shown = deck.name
	}
	recorded := appWiring{
		open:     driveWire(deck),
		wires:    func() []tui.Wire { return []tui.Wire{{Name: wireSubscription, Model: shown, Provider: cassetteBuild}} },
		blockers: func() []tui.Requirement { return nil },
	}
	return tui.New(appOptions(dir, arms, recorded, sessionResume{}))
}

type driveStep struct {
	verb string
	text string
	line int
}

func readScript(path string, in io.Reader) ([]driveStep, error) {
	if path != stdinScript {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = file.Close() }()
		in = file
	}
	body, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	var steps []driveStep
	for number, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		verb, text, _ := strings.Cut(trimmed, " ")
		steps = append(steps, driveStep{verb: verb, text: strings.TrimSpace(text), line: number + 1})
	}
	return steps, nil
}

type drivePlan struct {
	script   string
	dir      string
	home     string
	cassette string
	width    int
	height   int
	timeout  time.Duration
	plain    bool
	arms     runOpts
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
		case "--no-gate":
			plan.arms.gateArm = gateOff
			continue
		case "--no-subagents":
			plan.arms.noSubAgents = true
			continue
		case "--no-instructions":
			plan.arms.noInstructions = true
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
	driver := filmstrip.Drive(drivenApp(dir, deck, plan.arms), plan.width, plan.height)
	defer driver.Close()
	for _, step := range steps {
		driver.Settle()
		if code := playStep(driver, deck, step, plan, out, errOut); code != exitOK {
			return code
		}
	}
	return exitOK
}

func leadingCells(fields []string, count, least int) ([]int, []string, bool) {
	if len(fields) < count {
		return nil, nil, false
	}
	cells := make([]int, count)
	for index := range cells {
		cell, err := strconv.Atoi(fields[index])
		if err != nil || cell < least {
			return nil, nil, false
		}
		cells[index] = cell
	}
	return cells, fields[count:], true
}

func pointerStep(driver *filmstrip.Driver, verb string, fields []string) error {
	switch verb {
	case "click":
		if at, mods, ok := leadingCells(fields, 2, 0); ok {
			return driver.Click(at[0], at[1], mods...)
		}
		return errors.New("the step is click X Y [alt] [shift] [ctrl], with X and Y cells from 0")
	case "drag":
		if at, mods, ok := leadingCells(fields, 4, 0); ok {
			return driver.Drag(at[0], at[1], at[2], at[3], mods...)
		}
		return errors.New("the step is drag X1 Y1 X2 Y2 [alt] [shift] [ctrl], with every X and Y a cell from 0")
	case "wheel":
		at, turn, ok := leadingCells(fields, 2, 0)
		if ok && len(turn) == 1 {
			turn = append(turn, "1")
		}
		if ok && len(turn) == 2 && (turn[0] == "up" || turn[0] == "down") {
			if notches, _, counted := leadingCells(turn[1:], 1, 1); counted {
				return driver.Wheel(at[0], at[1], turn[0] == "up", notches[0])
			}
		}
		return errors.New("the step is wheel X Y up|down [N], with N notches from 1")
	}
	size, rest, ok := leadingCells(fields, 2, 1)
	if !ok || len(rest) > 0 {
		return errors.New("the step is resize W H, with W columns and H rows from 1")
	}
	driver.Resize(size[0], size[1])
	return nil
}

func playStep(driver *filmstrip.Driver, deck *cassette, step driveStep, plan drivePlan, out, errOut io.Writer) int {
	switch step.verb {
	case "type":
		driver.Type(step.text)
	case "key":
		if err := driver.Press(step.text); err != nil {
			return driveFail(errOut, fmt.Errorf("line %d: %w", step.line, err))
		}
	case "wait":
		if err := driver.Await(step.text, plan.timeout); err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu drive: line %d: %v\n\n%s\n", step.line, err, driver.Plain())
			return exitVerdict
		}
	case "screen":
		screen := driver.Screen()
		if plan.plain {
			screen = driver.Plain()
		}
		_, _ = fmt.Fprintln(out, screen)
	case "environment":
		_, _ = fmt.Fprintln(out, deck.environment())
	case "click", "drag", "wheel", "resize":
		if err := pointerStep(driver, step.verb, strings.Fields(step.text)); err != nil {
			return driveFail(errOut, fmt.Errorf("line %d: %w", step.line, err))
		}
	default:
		return driveFail(errOut, fmt.Errorf("line %d: no step is named %q", step.line, step.verb))
	}
	return exitOK
}
