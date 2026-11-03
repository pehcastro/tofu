package turn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func bashPlace(command, cwd string) string {
	args, _ := json.Marshal(map[string]string{"command": command})
	return CallPlace(GateRequest{Tool: bashToolName, Args: args}, cwd)
}

func TestCallPlaceKeysInterpreterCodeByItsBytes(t *testing.T) {
	project, other := t.TempDir(), t.TempDir()
	first := bashPlace(`python -c "import os; print(1)"`, project)
	if first == bashPlace(`python -c "import os; print(2)"`, project) {
		t.Errorf("python -c with different code shares the place %q", first)
	}
	if first != bashPlace(`python -c "import os; print(1)"`, project) {
		t.Errorf("the same python -c moved its place, so an always could never hold")
	}
	if first == bashPlace(`python -c "import os; print(1)"`, other) {
		t.Errorf("the same code from another cwd shares the place %q", first)
	}
	if !PlacesCode(first) {
		t.Errorf("%q is not recognised as a code place", first)
	}
	wrapped := bashPlace(`uv run python -c "import os; print(1)"`, project)
	if wrapped == bashPlace(`uv run python -c "import os; print(3)"`, project) {
		t.Errorf("uv run python with different code shares the place %q", wrapped)
	}
	script := filepath.Join(project, "x.py")
	if err := os.WriteFile(script, []byte("print(1)"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := bashPlace("cd . && python x.py", project)
	if err := os.WriteFile(script, []byte("import shutil; shutil.rmtree('/')"), 0o600); err != nil {
		t.Fatal(err)
	}
	if before == bashPlace("cd . && python x.py", project) {
		t.Errorf("python x.py kept its place after x.py was rewritten")
	}
	if got := bashPlace("go test -run X ./...", project); got != "bash go test -run" || PlacesCode(got) {
		t.Errorf("go test -run X ./... is placed %q, want today's bash go test -run", got)
	}
}
