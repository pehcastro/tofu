package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tofu/internal/turn"
	"tofu/internal/turn/tools"
)

func checkedGrep(t *testing.T, root string) turn.Tool {
	t.Helper()
	grepTool, err := tools.NewGrep(root)
	if err != nil {
		t.Fatalf("building grep: %v", err)
	}
	checked, err := tools.Checked(root, []turn.Tool{grepTool})
	if err != nil {
		t.Fatalf("wrapping grep: %v", err)
	}
	return checked[0]
}

func TestTheResultTheModelGetsCarriesTheCitationVerdict(t *testing.T) {
	scratch := t.TempDir()
	seed(t, scratch, "notes.md", "the cost row is at ledger/write.go:28 and the loop is at loop.go:1\n")
	seed(t, scratch, "loop.go", "package loop\n")

	result, err := checkedGrep(t, scratch).Run(context.Background(), json.RawMessage(`{"pattern":"cost row"}`))
	if err != nil {
		t.Fatalf("running the checked grep: %v", err)
	}
	if !strings.Contains(result.Content, "refused ledger/write.go:28 no_file: no file is at that path under the working directory") {
		t.Fatalf("the model would read this, and the fabricated citation is not refused in it:\n%s", result.Content)
	}
	if !strings.Contains(result.Content, "citations: 3 found, 2 resolved, 1 refused") {
		t.Fatalf("the model would read this, and it does not count the citations:\n%s", result.Content)
	}
}

func TestAResultWithNoCitationIsLeftAlone(t *testing.T) {
	scratch := t.TempDir()
	seed(t, scratch, "notes.md", "the cost row is written once and read twice\n")

	plain, err := tools.NewGrep(scratch)
	if err != nil {
		t.Fatalf("building grep: %v", err)
	}
	args := json.RawMessage(`{"pattern":"nothing matches this"}`)
	bare, err := plain.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("running grep: %v", err)
	}
	checked, err := checkedGrep(t, scratch).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("running the checked grep: %v", err)
	}
	if checked.Content != bare.Content {
		t.Fatalf("a result with no citation was changed:\n%s\nagainst\n%s", checked.Content, bare.Content)
	}
}
