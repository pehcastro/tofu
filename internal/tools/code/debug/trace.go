package debug

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/tools/code/dap"
)

const (
	traceCalls        = 20
	traceInstructions = 4096
)

type call struct {
	args     []string
	results  []string
	from     string
	entered  time.Time
	took     time.Duration
	returned bool
}

type tracer struct {
	s       *session
	symbol  string
	name    string
	entry   int
	returns []int
	calls   []call
	count   int
	open    map[int][]int
}

func (s *session) trace(ctx context.Context, symbol string) (string, error) {
	switch {
	case symbol == "":
		return "", errors.New("trace needs a function, such as orders.Order.Total")
	case s.program != "dlv":
		return "", errors.New("trace follows go functions under dlv, and a python trace is not built yet: break inside the function and run instead")
	case s.configured:
		return "", errors.New("trace runs the program from its start: start it again, then trace")
	case len(s.breaks) > 0:
		return "", errors.New("trace runs without breaks: start again and trace before any break")
	}
	running, cancel := context.WithTimeoutCause(ctx, s.limits.Run, errors.New("the traced program did not end within "+s.limits.Run.String()))
	defer cancel()
	var set struct {
		Breakpoints []struct {
			ID       int    `json:"id"`
			Verified bool   `json:"verified"`
			Message  string `json:"message"`
		} `json:"breakpoints"`
	}
	if err := s.client.Call(running, "setFunctionBreakpoints", map[string]any{"breakpoints": []map[string]any{{"name": symbol}}}, &set); err != nil {
		return "", err
	}
	if len(set.Breakpoints) != 1 || !set.Breakpoints[0].Verified {
		said := ""
		if len(set.Breakpoints) == 1 {
			said = ", " + s.program + " said " + set.Breakpoints[0].Message
		}
		return "", errors.New(s.program + " found no function " + symbol + ": name it with its package, such as orders.Order.Total" + said)
	}
	if err := s.configure(ctx); err != nil {
		return "", err
	}
	t := &tracer{s: s, symbol: symbol, entry: set.Breakpoints[0].ID, open: map[int][]int{}}
	for {
		event, err := s.next(running)
		if errors.Is(err, dap.ErrExited) {
			s.ended = true
			break
		}
		if err != nil {
			return "", s.explained(context.Cause(running), err)
		}
		if event.Name == "terminated" {
			break
		}
		if event.Name != "stopped" {
			continue
		}
		hit := s.stoppedBy(event).HitBreakpointIDs
		switch {
		case slices.Contains(hit, t.entry):
			err = t.enter(running)
		case slices.ContainsFunc(hit, func(id int) bool { return slices.Contains(t.returns, id) }):
			err = t.leave(running)
		}
		if err == nil {
			err = s.client.Call(running, "continue", map[string]any{"threadId": s.thread}, nil)
		}
		if err != nil {
			return "", s.explained(context.Cause(running), err)
		}
	}
	return t.report() + s.end(), nil
}

func (t *tracer) enter(ctx context.Context) error {
	t.count++
	thread := t.s.thread
	if len(t.calls) >= traceCalls {
		if len(t.returns) > 0 {
			t.open[thread] = append(t.open[thread], -1)
		}
		return nil
	}
	stack, err := t.s.stack(ctx, 2, "function breakpoint")
	if err != nil {
		return err
	}
	if t.name == "" {
		t.name = stack[0].Name
		if err := t.watchReturns(ctx, stack[0].IP); err != nil {
			return err
		}
	}
	held, err := t.s.variables(ctx, stack[0].ID)
	if err != nil {
		return err
	}
	traced := call{from: "outside go"}
	if len(stack) > 1 {
		traced.from = t.s.place(stack[1])
	}
	for _, argument := range held {
		if !strings.HasPrefix(argument.Name, "~r") {
			traced.args = append(traced.args, t.s.valued(argument.Name, argument.Value, argument.Type))
		}
	}
	traced.entered = time.Now()
	t.calls = append(t.calls, traced)
	if len(t.returns) > 0 {
		t.open[thread] = append(t.open[thread], len(t.calls)-1)
	}
	return nil
}

func (t *tracer) watchReturns(ctx context.Context, ip string) error {
	var listing struct {
		Instructions []struct {
			Address     string `json:"address"`
			Instruction string `json:"instruction"`
		} `json:"instructions"`
	}
	if err := t.s.client.Call(ctx, "disassemble", map[string]any{"memoryReference": ip, "instructionCount": traceInstructions}, &listing); err != nil {
		return err
	}
	var asked []map[string]any
	for _, held := range listing.Instructions {
		if strings.HasPrefix(held.Instruction, "JMP $") && strings.HasSuffix(held.Instruction, t.name+"(SB)") {
			break
		}
		if held.Instruction == "RET" {
			asked = append(asked, map[string]any{"instructionReference": held.Address})
		}
	}
	if len(asked) == 0 {
		return nil
	}
	var set struct {
		Breakpoints []struct {
			ID       int  `json:"id"`
			Verified bool `json:"verified"`
		} `json:"breakpoints"`
	}
	if err := t.s.client.Call(ctx, "setInstructionBreakpoints", map[string]any{"breakpoints": asked}, &set); err != nil {
		return err
	}
	for _, held := range set.Breakpoints {
		if held.Verified {
			t.returns = append(t.returns, held.ID)
		}
	}
	return nil
}

func (t *tracer) leave(ctx context.Context) error {
	stack, err := t.s.stack(ctx, 1, "instruction breakpoint")
	if err != nil {
		return err
	}
	thread := t.s.thread
	open := t.open[thread]
	if stack[0].Name != t.name || len(open) == 0 {
		return nil
	}
	index := open[len(open)-1]
	t.open[thread] = open[:len(open)-1]
	if len(t.open[thread]) == 0 {
		delete(t.open, thread)
	}
	if index >= 0 {
		returned := &t.calls[index]
		returned.took, returned.returned = time.Since(returned.entered), true
		held, err := t.s.variables(ctx, stack[0].ID)
		if err != nil {
			return err
		}
		for _, result := range held {
			if strings.HasPrefix(result.Name, "~r") {
				returned.results = append(returned.results, t.s.shortened(result.Value, result.Type))
			}
		}
	}
	if len(t.calls) < traceCalls || len(t.open) > 0 {
		return nil
	}
	t.returns = nil
	return t.s.client.Call(ctx, "setInstructionBreakpoints", map[string]any{"breakpoints": []any{}}, nil)
}

func (t *tracer) report() string {
	if t.count == 0 {
		return "trace " + t.symbol + ": 0 calls, it was never called in this run\n"
	}
	said := "trace " + t.symbol + ": " + strconv.Itoa(t.count) + " calls\n"
	if t.count == 1 {
		said = "trace " + t.symbol + ": 1 call\n"
	}
	for i, traced := range t.calls {
		result := "did not return"
		if traced.returned {
			result = fmt.Sprintf("returned %s, %.2f ms under the debugger", cmp.Or(strings.Join(traced.results, ", "), "nothing"), float64(traced.took.Microseconds())/1000)
		}
		said += fmt.Sprintf("  %d. %s, %s, from %s\n", i+1, cmp.Or(strings.Join(traced.args, ", "), "no arguments"), result, traced.from)
	}
	if t.count > len(t.calls) {
		said += fmt.Sprintf("  the first %d are shown, of %d\n", len(t.calls), t.count)
	}
	return said
}
