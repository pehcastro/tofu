package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/skill"
)

func TestSkillToolReturnsTheBodyAndRefusesTheRest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "commit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\ndescription: d\n---\nwrite feat: subjects"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewSkill([]skill.Skill{{Name: "commit", Description: "d", File: filepath.Join(dir, "SKILL.md"), Dir: dir}})
	if schema, _ := json.Marshal(tool.Definition().Parameters); tool.Name() != "skill" || !strings.Contains(string(schema), `"required":["name"]`) {
		t.Fatalf("definition %+v, want skill with name required", tool.Definition())
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"name":"commit"}`))
	if err != nil || result.Content != "write feat: subjects" || result.Command != "commit" {
		t.Fatalf("result %+v err %v, want the body without front matter", result, err)
	}
	for _, raw := range []string{`{"name":"commit","path":"../x"}`, `{"name":"other"}`, `{}`, `not json`} {
		if result, err := tool.Run(context.Background(), json.RawMessage(raw)); err == nil {
			t.Fatalf("%s returned %+v", raw, result)
		}
	}
	if !strings.Contains(tool.Definition().Description, "SKILL.md") {
		t.Fatal("the description does not say what comes back")
	}
}
