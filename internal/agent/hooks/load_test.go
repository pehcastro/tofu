package hook

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"tofu/internal/shell"
	"tofu/internal/sys"
)

func TestHooksReadACodexConfigTomlBesideItsOtherTablesAndNameTheLineOfABrokenOne(t *testing.T) {
	project := t.TempDir()
	config := filepath.Join(project, ".codex", "config.toml")
	write := func(body string) {
		t.Helper()
		if err := sys.WriteFile(config, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`model = "gpt-5.6"
updated = 2026-10-14T07:00:00Z
local_day = 2026-10-14

[mcp_servers.docs]
command = "npx"
args = ["-y", "docs-server"]

[[hooks.PreToolUse]]
matcher = "Bash"

[[hooks.PreToolUse.hooks]]
type = "command"
command = "echo whole"
timeout = 5

[[hooks.PreToolUse.hooks]]
type = "command"
command = "echo half"
timeout = 2.5

[[hooks.Stop]]

[[hooks.Stop.hooks]]
type = "command"
command = "echo stop"

[hooks.state]
trusted = ["abc"]
`)
	engine := Load(project, shell.Choice{})
	var read []string
	for _, hook := range engine.Hooks() {
		if hook.File == config {
			read = append(read, string(hook.Event)+" "+hook.Matcher+" "+hook.Command+" "+strings.Repeat("s", hook.Timeout))
		}
	}
	want := []string{"PreToolUse Bash echo whole sssss", "PreToolUse Bash echo half sss", "Stop  echo stop "}
	if !slices.Equal(read, want) {
		t.Errorf("config.toml read as %q, want %q", read, want)
	}
	if problems := engine.Problems(); len(problems) > 0 {
		t.Errorf("a readable config.toml gave problems %q", problems)
	}

	write("model = \"x\"\n\n[hooks\nbroken = \n")
	problems := Load(project, shell.Choice{}).Problems()
	if len(problems) != 1 || !strings.Contains(problems[0], config) || !strings.Contains(problems[0], "line 3") {
		t.Errorf("a broken config.toml gave problems %q, want one naming the file and line 3", problems)
	}
}
