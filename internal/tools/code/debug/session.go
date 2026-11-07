package debug

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/platform/safe"
	"tofu/internal/tools/code/dap"
	"tofu/internal/tools/contract"
)

type session struct {
	client     *dap.Client
	program    string
	root       string
	limits     Limits
	launched   chan error
	configured bool
	paused     bool
	ended      bool
	exitCode   *int
	thread     int
	frame      int
	breaks     map[string][]breakpoint
	output     []string
}

type breakpoint struct {
	line      int
	condition string
	id        int
	hit       bool
}

type frame struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	IP     string `json:"instructionPointerReference"`
	Source struct {
		Path string `json:"path"`
	} `json:"source"`
}

func open(ctx context.Context, limits Limits, root string, chosen language, launch map[string]any) (*session, error) {
	starting, cancel := context.WithTimeout(ctx, limits.Request)
	defer cancel()
	client, err := dap.Start(starting, chosen.adapter, root)
	if err != nil {
		return nil, err
	}
	s := &session{client: client, program: chosen.adapter.Program, root: root, limits: limits, launched: make(chan error, 1), breaks: map[string][]breakpoint{}}
	initialize := map[string]any{"clientID": "tofu", "adapterID": chosen.name, "linesStartAt1": true, "columnsStartAt1": true, "pathFormat": "path", "supportsVariableType": true}
	if err := client.Call(starting, "initialize", initialize, nil); err != nil {
		s.stop()
		return nil, err
	}
	launch["request"], launch["name"] = "launch", "tofu debug"
	pending, err := client.Send("launch", launch)
	if err != nil {
		s.stop()
		return nil, err
	}
	loading, cancelLoading := context.WithTimeoutCause(ctx, limits.Run, errors.New(chosen.adapter.Program+" was not ready after "+limits.Run.String()))
	defer cancelLoading()
	watching, failed := context.WithCancelCause(loading)
	defer failed(nil)
	safe.Go(platformproject.Crashes(root), "debug launch", func() {
		err := pending.Wait(context.Background(), nil)
		s.launched <- err
		if err != nil {
			failed(err)
		}
	})
	for {
		event, err := s.next(watching)
		if err != nil {
			s.stop()
			return nil, s.explained(context.Cause(watching), err)
		}
		if event.Name == "initialized" {
			return s, nil
		}
	}
}

func (s *session) explained(cause, err error) error {
	if cause != nil && !errors.Is(cause, context.Canceled) {
		err = cause
	}
	if tail := s.tail(); tail != "" {
		return fmt.Errorf("%w. the program printed:\n%s", err, tail)
	}
	return err
}

func (s *session) next(ctx context.Context) (dap.Event, error) {
	event, err := s.client.Next(ctx)
	if err != nil {
		return event, err
	}
	switch event.Name {
	case "output":
		var said struct {
			Category string `json:"category"`
			Output   string `json:"output"`
		}
		_ = json.Unmarshal(event.Body, &said)
		if said.Category == "stdout" || said.Category == "stderr" {
			s.output = append(s.output, strings.Split(strings.TrimRight(said.Output, "\r\n"), "\n")...)
			s.output = s.output[max(0, len(s.output)-s.limits.OutputLines):]
		}
	case "exited":
		var exited struct {
			ExitCode int `json:"exitCode"`
		}
		_ = json.Unmarshal(event.Body, &exited)
		s.exitCode = &exited.ExitCode
	case "terminated":
		s.ended = true
	}
	return event, nil
}

func (s *session) tail() string {
	if len(s.output) == 0 {
		return ""
	}
	return "  " + strings.Join(s.output, "\n  ")
}

func (s *session) shown(path string) string {
	if rel, inside := contract.Within(s.root, path); inside {
		return filepath.ToSlash(rel)
	}
	return path
}

func (s *session) breakAt(ctx context.Context, spec string) (string, error) {
	place, condition, _ := strings.Cut(spec, " --if ")
	place, condition = strings.TrimSpace(place), strings.TrimSpace(condition)
	cut := strings.LastIndex(place, ":")
	line, err := strconv.Atoi(place[cut+1:])
	if cut <= 0 || err != nil || line < 1 {
		return "", errors.New("break takes path:line, such as order.go:15, and optionally --if <expression>")
	}
	path, err := contract.Root(s.root).Resolve(place[:cut])
	if err != nil {
		return "", err
	}
	if !contract.IsFile(path) {
		return "", errors.New(place[:cut] + " is not a file")
	}
	breaks := append(slices.Clone(s.breaks[path]), breakpoint{line: line, condition: condition})
	answer, err := s.setBreakpoints(ctx, path, breaks)
	if err != nil {
		return "", err
	}
	set := answer[len(breaks)-1]
	refusesUnplaced := s.program == delve().adapter.Program
	if refusesUnplaced && !set.Verified && set.Message != "" {
		return "", s.refused(ctx, path, line, set.Message)
	}
	for i, set := range answer {
		breaks[i].id = set.ID
	}
	s.breaks[path] = breaks
	said := "break " + s.shown(path) + ":" + strconv.Itoa(line)
	if condition != "" {
		said += " if " + condition
	}
	if !set.Verified && set.Message != "" {
		return said + ": not set yet, " + s.program + " said " + set.Message + "\n", nil
	}
	if set.Line != 0 && set.Line != line {
		return said + ": set at line " + strconv.Itoa(set.Line) + ", the nearest line that runs\n", nil
	}
	return said + ": set\n", nil
}

type placed struct {
	ID       int    `json:"id"`
	Verified bool   `json:"verified"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

func (s *session) setBreakpoints(ctx context.Context, path string, breaks []breakpoint) ([]placed, error) {
	asked := make([]map[string]any, len(breaks))
	for i, held := range breaks {
		asked[i] = map[string]any{"line": held.line}
		if held.condition != "" {
			asked[i]["condition"] = held.condition
		}
	}
	var answer struct {
		Breakpoints []placed `json:"breakpoints"`
	}
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	if err := s.client.Call(requesting, "setBreakpoints", map[string]any{"source": map[string]any{"path": path}, "breakpoints": asked}, &answer); err != nil {
		return nil, err
	}
	if len(answer.Breakpoints) != len(breaks) {
		return nil, fmt.Errorf("%s answered %d breakpoints for the %d asked in %s", s.program, len(answer.Breakpoints), len(breaks), s.shown(path))
	}
	return answer.Breakpoints, nil
}

func (s *session) refused(ctx context.Context, path string, line int, message string) error {
	said := s.program + " could not set it: " + message + ". "
	var window []breakpoint
	for probe := max(1, line-konst.DebugStatementProbeLines); probe <= line+konst.DebugStatementProbeLines; probe++ {
		window = append(window, breakpoint{line: probe})
	}
	probed, probeErr := s.setBreakpoints(ctx, path, window)
	restored, err := s.setBreakpoints(ctx, path, s.breaks[path])
	if err != nil {
		return errors.New(said + "putting back the other breaks in " + s.shown(path) + " failed: " + err.Error())
	}
	for i, set := range restored {
		s.breaks[path][i].id = set.ID
	}
	if probeErr != nil {
		return errors.New(said + "the program has not run")
	}
	before, after := 0, 0
	for _, probe := range probed {
		switch {
		case !probe.Verified:
		case probe.Line < line:
			before = probe.Line
		case probe.Line > line && after == 0:
			after = probe.Line
		}
	}
	var near []string
	for _, statement := range []int{before, after} {
		if statement != 0 {
			near = append(near, s.shown(path)+":"+strconv.Itoa(statement))
		}
	}
	if len(near) == 0 {
		return errors.New(said + "no line within " + strconv.Itoa(konst.DebugStatementProbeLines) + " of it holds a statement, and the program has not run")
	}
	return errors.New(said + "the nearest lines with a statement are " + strings.Join(near, " and ") + ", and the program has not run")
}

func (s *session) run(ctx context.Context) (string, error) {
	if s.configured {
		return s.move(ctx, "continue")
	}
	if err := s.configure(ctx); err != nil {
		return "", err
	}
	return s.resume(ctx)
}

func (s *session) configure(ctx context.Context) error {
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	s.configured = true
	if err := s.client.Call(requesting, "configurationDone", nil, nil); err != nil {
		return err
	}
	select {
	case err := <-s.launched:
		if err != nil {
			return s.explained(nil, err)
		}
	case <-requesting.Done():
		return errors.New(s.program + " did not finish launching after " + s.limits.Request.String())
	}
	return nil
}

func (s *session) move(ctx context.Context, command string) (string, error) {
	if err := s.stopped(); err != nil {
		return "", err
	}
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	if err := s.client.Call(requesting, command, map[string]any{"threadId": s.thread}, nil); err != nil {
		return "", err
	}
	return s.resume(ctx)
}

func (s *session) stopped() error {
	switch {
	case s.ended:
		return errors.New("the program has ended: start it again")
	case !s.paused:
		return errors.New("the program is not stopped: break somewhere, then run")
	}
	return nil
}

func (s *session) resume(ctx context.Context) (string, error) {
	s.paused = false
	running, cancel := context.WithTimeoutCause(ctx, s.limits.Run, errors.New("the program neither stopped nor ended within "+s.limits.Run.String()))
	defer cancel()
	for {
		event, err := s.next(running)
		if errors.Is(err, dap.ErrExited) {
			s.ended = true
			return s.end(), nil
		}
		if err != nil {
			return "", s.explained(context.Cause(running), err)
		}
		switch event.Name {
		case "terminated":
			return s.end(), nil
		case "stopped":
			stop := s.stoppedBy(event)
			return s.at(ctx, stop.Reason, stop.HitBreakpointIDs)
		}
	}
}

type stop struct {
	Reason           string `json:"reason"`
	ThreadID         int    `json:"threadId"`
	HitBreakpointIDs []int  `json:"hitBreakpointIds"`
}

func (s *session) stoppedBy(event dap.Event) stop {
	var stopped stop
	_ = json.Unmarshal(event.Body, &stopped)
	s.thread = stopped.ThreadID
	return stopped
}

func (s *session) end() string {
	said := "the program ended"
	if s.exitCode != nil {
		said += " with exit code " + strconv.Itoa(*s.exitCode)
	}
	said += "\n"
	for _, path := range slices.Sorted(maps.Keys(s.breaks)) {
		for _, held := range s.breaks[path] {
			if !held.hit {
				said += "break " + s.shown(path) + ":" + strconv.Itoa(held.line) + " was never hit"
				if held.condition != "" {
					said += ", or its condition " + held.condition + " was never true"
				}
				said += "\n"
			}
		}
	}
	if tail := s.tail(); tail != "" {
		said += "the program printed:\n" + tail + "\n"
	}
	return said
}

func (s *session) at(ctx context.Context, reason string, hit []int) (string, error) {
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	stack, err := s.stack(requesting, s.limits.Frames, reason)
	if err != nil {
		return "", err
	}
	top := stack[0]
	s.frame, s.paused = top.ID, true
	for path, breaks := range s.breaks {
		for i, held := range breaks {
			if slices.Contains(hit, held.id) || (held.line == top.Line && strings.EqualFold(filepath.Clean(path), filepath.Clean(top.Source.Path))) {
				breaks[i].hit = true
			}
		}
	}
	locals, err := s.locals(ctx)
	if err != nil {
		return "", err
	}
	said := "stopped at " + s.place(top) + ", on " + reason + "\n" + locals + "stack:\n"
	for _, held := range stack {
		said += "  " + s.place(held) + "\n"
	}
	return said, nil
}

func (s *session) stack(ctx context.Context, levels int, reason string) ([]frame, error) {
	var trace struct {
		StackFrames []frame `json:"stackFrames"`
	}
	if err := s.client.Call(ctx, "stackTrace", map[string]any{"threadId": s.thread, "startFrame": 0, "levels": levels}, &trace); err != nil {
		return nil, err
	}
	if len(trace.StackFrames) == 0 {
		return nil, errors.New(s.program + " stopped on " + reason + " with no stack")
	}
	return trace.StackFrames, nil
}

func (s *session) place(at frame) string {
	return s.shown(at.Source.Path) + ":" + strconv.Itoa(at.Line) + " in " + at.Name
}

func (s *session) locals(ctx context.Context) (string, error) {
	if err := s.stopped(); err != nil {
		return "", err
	}
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	held, err := s.variables(requesting, s.frame)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, variable := range held {
		lines = append(lines, "  "+s.valued(variable.Name, variable.Value, variable.Type))
	}
	if len(lines) == 0 {
		return "locals: none\n", nil
	}
	if len(lines) > s.limits.Locals {
		lines = append(lines[:s.limits.Locals], "  and "+strconv.Itoa(len(lines)-s.limits.Locals)+" more")
	}
	return "locals:\n" + strings.Join(lines, "\n") + "\n", nil
}

type variable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func (s *session) variables(ctx context.Context, frameID int) ([]variable, error) {
	var scopes struct {
		Scopes []struct {
			Name               string `json:"name"`
			PresentationHint   string `json:"presentationHint"`
			VariablesReference int    `json:"variablesReference"`
			Expensive          bool   `json:"expensive"`
		} `json:"scopes"`
	}
	if err := s.client.Call(ctx, "scopes", map[string]any{"frameId": frameID}, &scopes); err != nil {
		return nil, err
	}
	var all []variable
	for _, scope := range scopes.Scopes {
		if scope.Expensive || !slices.Contains([]string{"locals", "arguments", "Locals", "Arguments"}, cmp.Or(scope.PresentationHint, scope.Name)) {
			continue
		}
		var held struct {
			Variables []variable `json:"variables"`
		}
		if err := s.client.Call(ctx, "variables", map[string]any{"variablesReference": scope.VariablesReference}, &held); err != nil {
			return nil, err
		}
		all = append(all, held.Variables...)
	}
	return all, nil
}

func (s *session) valued(name, value, kind string) string {
	return name + " = " + s.shortened(value, kind)
}

func (s *session) shortened(value, kind string) string {
	flat := []rune(strings.Join(strings.Fields(value), " "))
	if len(flat) > s.limits.ValueRunes {
		flat = append(flat[:s.limits.ValueRunes], []rune("...")...)
	}
	if kind == "" {
		return string(flat)
	}
	return string(flat) + " (" + kind + ")"
}

func (s *session) eval(ctx context.Context, expression string) (string, error) {
	if expression == "" {
		return "", errors.New("eval needs an expression")
	}
	if err := s.stopped(); err != nil {
		return "", err
	}
	requesting, cancel := context.WithTimeout(ctx, s.limits.Request)
	defer cancel()
	var answer struct {
		Result string `json:"result"`
		Type   string `json:"type"`
	}
	if err := s.client.Call(requesting, "evaluate", map[string]any{"expression": expression, "frameId": s.frame, "context": "watch"}, &answer); err != nil {
		return "", err
	}
	return s.valued(expression, answer.Result, answer.Type) + "\n", nil
}

func (s *session) stop() string {
	stopping, cancel := context.WithTimeout(context.Background(), s.limits.Stop)
	defer cancel()
	if s.client.Stop(stopping) {
		return s.program + " and the program it ran have exited\n"
	}
	return s.program + " did not exit within " + s.limits.Stop.String() + " and was killed with the program it ran\n"
}
