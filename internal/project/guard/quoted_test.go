package guard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotedMessageIsTextNotGit(t *testing.T) {
	cases := []struct {
		name, agent, command, want string
	}{
		{name: "log quoting git prune", agent: "a1", command: `tofu boardy log TOFU-1 --board TOFU "snapshot.Prune runs git prune hourly"`},
		{name: "log quoting git commit and Uncommitted", agent: "a1", command: `tofu boardy log TOFU-1 --board TOFU "Uncommitted work is lost when git commit is skipped"`},
		{name: "log in single quotes quoting git reset --hard", agent: "a1", command: `tofu boardy log TOFU-1 'a git reset --hard ran mid-session'`},
		{name: "log after cd quoting git gc", agent: "a1", command: `cd /f/x && tofu boardy log TOFU-1 "git gc and git prune both ran"`},
		{name: "env assignment before a log quoting git push", agent: "a1", command: `TOFU_HOME=/tmp/h tofu boardy log TOFU-1 "never git push"`},
		{name: "orchestrator log quoting git commit gets no note", command: `tofu boardy log TOFU-1 "Committed: git commit fe512206"`},
		{name: "orchestrator log quoting git stash", command: `tofu boardy log TOFU-1 "a git stash -u reverted the tree"`},
		{name: "orchestrator log quoting git worktree", command: `tofu boardy log TOFU-1 "git worktree is refused"`},
		{name: "grep for the text git prune", agent: "a1", command: `grep -rn "git prune" internal/snapshot`},
		{name: "echo of git prune piped into sh", agent: "a1", command: `echo "git prune" | sh`, want: "Git may not change"},
		{name: "quoted git prune passed to another program", agent: "a1", command: `watch "git prune"`, want: "Git may not change"},
		{name: "real git prune", agent: "a1", command: "git prune", want: "Git may not change"},
		{name: "real git prune after a log", agent: "a1", command: `tofu boardy log TOFU-1 "x" && git prune`, want: "Git may not change"},
		{name: "git commit with a quoted message", agent: "a1", command: `git commit -m "fix: a thing"`, want: "Git may not change"},
		{name: "bash -c body running git prune", agent: "a1", command: `bash -c "git prune"`, want: "Git may not change"},
		{name: "sh -c body running git commit", agent: "a1", command: `sh -c 'git commit -m x'`, want: "Git may not change"},
		{name: "bash.exe -c body running git prune", agent: "a1", command: `/usr/bin/bash.exe -c "git prune"`, want: "Git may not change"},
		{name: "env before bash -c running git prune", agent: "a1", command: `env A=1 bash -c "git prune"`, want: "Git may not change"},
		{name: "powershell -Command running git prune", agent: "a1", command: `powershell -Command "git prune"`, want: "Git may not change"},
		{name: "eval of a quoted git prune", agent: "a1", command: `eval "git prune"`, want: "Git may not change"},
		{name: "xargs running a quoted git prune", agent: "a1", command: `echo x | xargs sh -c "git prune"`, want: "Git may not change"},
		{name: "command substitution inside a quoted message", agent: "a1", command: `tofu boardy log TOFU-1 "$(git prune)"`, want: "Git may not change"},
		{name: "backtick substitution inside a quoted message", agent: "a1", command: "tofu boardy log TOFU-1 \"`git prune`\"", want: "Git may not change"},
		{name: "orchestrator bash -c reset --hard", command: `bash -c "git reset --hard"`, want: "discards uncommitted work"},
		{name: "orchestrator sh -c worktree", command: `sh -c "git worktree add y"`, want: "git worktree is refused"},
	}
	for _, c := range cases {
		stdin, _ := json.Marshal(map[string]any{"tool_name": "Bash", "agent_id": c.agent, "tool_input": map[string]string{"command": c.command}})
		answer, err := Hook(Shell, Env{Root: t.TempDir(), Temp: t.TempDir()}, stdin)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case c.want == "" && answer != Answer{}:
			t.Errorf("%s: refused %q: code %d %s%s", c.name, c.command, answer.Code, answer.Stdout, answer.Stderr)
		case c.want != "" && !strings.Contains(answer.Stdout, c.want):
			t.Errorf("%s: %q was not refused for %q: %s", c.name, c.command, c.want, answer.Stdout)
		}
	}
}
