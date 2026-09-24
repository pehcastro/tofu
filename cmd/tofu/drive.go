package main

import (
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

const driveUsage = `usage: tofu drive [SCRIPT] [--dir PATH] [--cassette PATH] [--width N] [--height N] [--timeout 60s] [--plain]

drives the app the way a person does, with no terminal and no model call.
SCRIPT is a file of steps, or - for standard input.

Steps:
  type TEXT    type TEXT into the composer
  key NAME     press enter, esc, tab, space, backspace, up, down, left, right,
               any single character, or one of those under ctrl+, alt+ or shift+
               (alt+2 opens work, alt+5 opens shells, as in the app)
  wait TEXT    wait until TEXT is on the screen, and fail saying so if it never is
  screen       print the screen as it stands
  environment  print the environment block the last turn sent to the model
  # NOTE       a note, skipped

The model is a cassette, one recorded reply per line of json:
  {"text":"reading it","tools":[{"name":"read","args":{"path":"note.txt"}}]}
  {"text":"the note says a note"}

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
	noEnvironment    = "no turn has sent an environment block yet"
)

type cassetteReply struct {
	Text  string `json:"text"`
	Tools []struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"tools"`
}

type cassette struct {
	name    string
	replies []llm.Decision
	mutex   sync.Mutex
	asked   int
	last    llm.Request
}

func readCassette(path string) (*cassette, error) {
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	deck := &cassette{name: filepath.Base(path)}
	for number, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var reply cassetteReply
		if err := json.Unmarshal([]byte(trimmed), &reply); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, number+1, err)
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
		deck.replies = append(deck.replies, decision)
	}
	if len(deck.replies) == 0 {
		return nil, fmt.Errorf("%s holds no reply", path)
	}
	return deck, nil
}

func (c *cassette) Ask(_ context.Context, request llm.Request) (llm.Decision, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.last = request
	if c.asked >= len(c.replies) {
		return llm.Decision{}, fmt.Errorf("%s holds %d replies and the turn asked for one more", c.name, len(c.replies))
	}
	c.asked++
	return c.replies[c.asked-1], nil
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

func drivenApp(dir string, deck *cassette) *tui.App {
	shown := cassetteBuild
	if deck != nil {
		shown = deck.name
	}
	recorded := appWiring{
		open:     driveWire(deck),
		wires:    func() []tui.Wire { return []tui.Wire{{Name: wireSubscription, Model: shown, Provider: cassetteBuild}} },
		blockers: func() []tui.Requirement { return nil },
	}
	return tui.New(appOptions(dir, recorded, sessionResume{}))
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
	cassette string
	width    int
	height   int
	timeout  time.Duration
	plain    bool
}

func driveArgs(args []string, errOut io.Writer) (drivePlan, bool) {
	plan := drivePlan{
		script:   stdinScript,
		cassette: os.Getenv(cassetteVariable),
		width:    fixture.Width,
		height:   fixture.Height,
		timeout:  konst.DriveTimeoutMillis * time.Millisecond,
	}
	for index := 0; index < len(args); index++ {
		flag := args[index]
		if flag == "--plain" {
			plan.plain = true
			continue
		}
		if !strings.HasPrefix(flag, "--") {
			plan.script = flag
			continue
		}
		index++
		if index >= len(args) {
			_, _ = fmt.Fprintf(errOut, "tofu drive: %s wants a value\n\n%s", flag, driveUsage)
			return drivePlan{}, false
		}
		taken := args[index]
		var err error
		switch flag {
		case "--dir":
			plan.dir = taken
		case "--cassette":
			plan.cassette = taken
		case "--width":
			plan.width, err = strconv.Atoi(taken)
		case "--height":
			plan.height, err = strconv.Atoi(taken)
		case "--timeout":
			plan.timeout, err = time.ParseDuration(taken)
		default:
			err = errors.New("no such flag")
		}
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "tofu drive: %s %q: %v\n\n%s", flag, taken, err, driveUsage)
			return drivePlan{}, false
		}
	}
	return plan, true
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
	steps, err := readScript(plan.script, in)
	if err != nil {
		return driveFail(errOut, err)
	}
	deck, err := readCassette(plan.cassette)
	if err != nil {
		return driveFail(errOut, err)
	}
	driver := filmstrip.Drive(drivenApp(dir, deck), plan.width, plan.height)
	defer driver.Close()
	for _, step := range steps {
		driver.Settle()
		if code := playStep(driver, deck, step, plan, out, errOut); code != exitOK {
			return code
		}
	}
	return exitOK
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
	default:
		return driveFail(errOut, fmt.Errorf("line %d: no step is named %q", step.line, step.verb))
	}
	return exitOK
}
