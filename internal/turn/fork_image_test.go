package turn

import (
	"slices"
	"strings"
	"testing"

	"tofu/internal/llm"
	"tofu/internal/recall"
)

func TestAForkKeepsTheImageOfTheTaskItCarriesAndNoOther(t *testing.T) {
	artifacts, err := NewArtifacts(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := recall.BudgetFor("m1", 0)
	if err != nil {
		t.Fatal(err)
	}
	old, flag := llm.Image{MediaType: "image/png", Data: []byte("old")}, llm.Image{MediaType: "image/png", Data: []byte("flag")}
	const task = "[Image #1] what does this image show?"
	messages := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "<env>cwd</env>\n\nan older question", Images: []llm.Image{old}},
		{Role: llm.RoleAssistant, Content: strings.Repeat("an older answer ", 4000)},
		{Role: llm.RoleUser, Content: "<env>cwd</env>\n\n" + task, Images: []llm.Image{flag}},
	}
	_, begun, err := forkHistory(artifacts, budget, task, messages, ForkContinuation, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	var carried []llm.Image
	for _, message := range begun {
		carried = append(carried, message.Images...)
	}
	opening := slices.IndexFunc(begun, func(message llm.Message) bool { return message.Role == llm.RoleUser && message.Content == task })
	if opening < 0 || len(begun[opening].Images) != 1 || string(begun[opening].Images[0].Data) != "flag" {
		t.Fatalf("the fork's task message lost the image the person sent with it: %d images on it", len(begun[max(opening, 0)].Images))
	}
	if len(carried) != 1 {
		t.Fatalf("the fork carries %d images, want only the task's own", len(carried))
	}
}
