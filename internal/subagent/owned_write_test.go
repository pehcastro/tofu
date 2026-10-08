package subagent_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/subagent"
	"tofu/internal/turn"
)

func TestAWriteToolCallOnTmpCreatesNothingInTheProject(t *testing.T) {
	project := t.TempDir()
	tool, err := turn.NewWriteTool(project)
	if err != nil {
		t.Fatal(err)
	}
	boundary := subagent.NewBoundary("ts-dev-1", "", []string{"apps/web/src/routes/components/$slug.tsx"})
	raw, _ := json.Marshal(map[string]string{"path": "/tmp/tofu-959.txt", "content": "x"})
	refused := boundary.Write("/tmp/tofu-959.txt")
	if refused == nil {
		_, refused = tool.Run(context.Background(), raw)
	}
	t.Logf("write /tmp/tofu-959.txt -> %v", refused)
	if _, err := os.Stat(filepath.Join(project, "tmp", "tofu-959.txt")); err == nil {
		t.Errorf("the write tool created %s inside the project", filepath.Join(project, "tmp", "tofu-959.txt"))
	}
	if refused == nil {
		t.Error("a write tool call on /tmp/tofu-959.txt: want refused, got allowed")
	}
}
