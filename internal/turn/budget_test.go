package turn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"tofu/internal/llm"
)

type ranShell struct{ ran *[]string }

func (ranShell) Name() string         { return "bash" }
func (ranShell) Definition() llm.Tool { return llm.Tool{Name: "bash"} }
func (s ranShell) Run(_ context.Context, raw json.RawMessage) (Result, error) {
	*s.ran = append(*s.ran, string(raw))
	return Result{}, nil
}

func TestShellWriteToSourceIsRefusedWithNothingSpent(t *testing.T) {
	var ran []string
	shell := WithSourceBudget([]Tool{ranShell{ran: &ran}}, nil)[0]
	for _, c := range []struct {
		command string
		refused bool
	}{
		{"sed -i s/a/b/ src/app.ts", true},
		{"echo x >> NOTES.md", false},
	} {
		raw, _ := json.Marshal(map[string]string{"command": c.command})
		_, err := shell.Run(context.Background(), raw)
		var refusal SourceBudgetError
		if errors.As(err, &refusal) != c.refused {
			t.Errorf("%q: refused %v, want %v, error %v", c.command, err != nil, c.refused, err)
		}
	}
	if len(ran) != 1 {
		t.Errorf("ran %d commands, want only the markdown write: %v", len(ran), ran)
	}
}
