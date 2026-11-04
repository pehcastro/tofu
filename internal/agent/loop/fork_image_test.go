package loop

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/agent/context/recall"
	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
)

func TestAForkKeepsTheImageOfTheTaskItCarriesAndNoOther(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := recall.BudgetUnder("m1", 0, konst.ContextCeilingTokens)
	if err != nil {
		t.Fatal(err)
	}
	old, flag := core.Image{MediaType: "image/png", Data: []byte("old")}, core.Image{MediaType: "image/png", Data: []byte("flag")}
	const task = "[Image #1] what does this image show?"
	messages := []core.Message{
		{Role: core.RoleSystem, Content: "system"},
		{Role: core.RoleUser, Content: "<env>cwd</env>\n\nan older question", Images: []core.Image{old}},
		{Role: core.RoleAssistant, Content: strings.Repeat("an older answer ", 4000)},
		{Role: core.RoleUser, Content: "<env>cwd</env>\n\n" + task, Images: []core.Image{flag}},
	}
	_, begun, err := forkHistory(artifacts, budget, taskOpening(task), messages, ForkContinuation, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var carried []core.Image
	for _, message := range begun {
		carried = append(carried, message.Images...)
	}
	holder := slices.IndexFunc(begun, func(message core.Message) bool { return len(message.Images) > 0 })
	if holder < 0 || begun[holder].Role != core.RoleUser || !strings.Contains(begun[holder].Content, task) || string(begun[holder].Images[0].Data) != "flag" {
		t.Fatalf("the fork's task message lost the image the person sent with it: %d images carried", len(carried))
	}
	if len(carried) != 1 {
		t.Fatalf("the fork carries %d images, want only the task's own", len(carried))
	}
}
