package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptGitIsRefusedLikePlainGit(t *testing.T) {
	root, temp := t.TempDir(), t.TempDir()
	files := map[string]string{
		filepath.Join(root, "setup.sh"):          "#!/bin/sh\ngit init\ngit add -A && git commit -m init\n",
		filepath.Join(temp, "drive", "setup.py"): "import subprocess\nsubprocess.run([\"git\", \"init\"])\nsubprocess.run([\"git\", \"add\", \"-A\"])\nsubprocess.run([\"git\", \"commit\", \"-m\", \"seed\"])\n",
		filepath.Join(root, "seed.js"):           "const { spawnSync } = require('child_process')\nspawnSync('git', ['-c', 'user.name=x', 'commit', '-m', 'seed'])\n",
		filepath.Join(root, "reads.py"):          "import subprocess\nprint(subprocess.check_output([\"git\", \"log\", \"--oneline\", \"-5\"]))\nprint(subprocess.check_output([\"git\", \"status\", \"--short\"]))\n",
		filepath.Join(root, "wipe.sh"):           "git reset --hard\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, agent, command, want string
	}{
		{name: "python heredoc runs git add and commit", agent: "a1", command: "python3 - <<'EOF'\nimport subprocess\nsubprocess.run([\"git\", \"add\", \"-A\"])\nsubprocess.run([\"git\", \"commit\", \"-m\", \"x\"])\nEOF", want: "Git may not change"},
		{name: "node heredoc runs git add", agent: "a1", command: "node <<EOF\nconst { execSync } = require('child_process')\nexecSync('git add .')\nEOF", want: "Git may not change"},
		{name: "bash heredoc runs git reset", agent: "a1", command: "bash <<'EOF'\ngit reset --hard\nEOF", want: "Git may not change"},
		{name: "python -c with a list form commit", agent: "a1", command: `python -c "import subprocess; subprocess.run(['git', 'commit', '-m', 'x'])"`, want: "Git may not change"},
		{name: "node -e with a list form reset", agent: "a1", command: `node -e "require('child_process').execFileSync('git', ['reset', '--hard'])"`, want: "Git may not change"},
		{name: "bash script file in the project", agent: "a1", command: "bash setup.sh", want: "Git may not change"},
		{name: "python script file in scratch by absolute path", agent: "a1", command: "python3 " + filepath.Join(temp, "drive", "setup.py"), want: "Git may not change"},
		{name: "cd into scratch then a python script file", agent: "a1", command: "cd " + filepath.Join(temp, "drive") + " && python setup.py", want: "Git may not change"},
		{name: "node script file with git -c", agent: "a1", command: "node ./seed.js --fast", want: "Git may not change"},
		{name: "script written by a heredoc and run in one command", agent: "a1", command: "cat > s.sh <<'EOF'\ngit commit -am x\nEOF\nbash s.sh", want: "Git may not change"},
		{name: "bash -c around python -c", agent: "a1", command: `bash -c "python -c \"import subprocess; subprocess.run(['git','add','-A'])\""`, want: "Git may not change"},
		{name: "orchestrator python heredoc resets hard", command: "python3 - <<'EOF'\nimport subprocess\nsubprocess.run([\"git\", \"reset\", \"--hard\"])\nEOF", want: "discards uncommitted work"},
		{name: "orchestrator bash script file resets hard", command: "bash wipe.sh", want: "discards uncommitted work"},
		{name: "python script file that only reads git output", agent: "a1", command: "python reads.py"},
		{name: "python -c that only reads git output", agent: "a1", command: `python -c "import subprocess; print(subprocess.check_output(['git', 'rev-parse', 'HEAD']))"`},
		{name: "python heredoc that only reads git output", agent: "a1", command: "python3 - <<'EOF'\nimport subprocess\nprint(subprocess.check_output([\"git\", \"diff\", \"--stat\"]))\nEOF"},
		{name: "a script file that does not exist", agent: "a1", command: "python3 missing.py"},
		{name: "a ticket log quoting git commit in a heredoc", agent: "a1", command: "tofu boardy log TOFU-1 <<'EOF'\nran git commit -m x by hand\nEOF"},
		{name: "cat printing a heredoc that names git reset", agent: "a1", command: "cat <<'EOF'\ngit reset --hard\nEOF"},
		{name: "grep for quoted git commit still allowed", agent: "a1", command: `grep -rn "git commit" .`},
	}
	for _, c := range cases {
		stdin, _ := json.Marshal(map[string]any{"tool_name": "Bash", "agent_id": c.agent, "tool_input": map[string]string{"command": c.command}})
		answer, err := Hook(Shell, Env{Root: root, Temp: temp}, stdin)
		if err != nil {
			t.Fatal(err)
		}
		denied := strings.Contains(answer.Stdout, `"deny"`)
		switch {
		case c.want == "" && denied:
			t.Errorf("%s: refused %q: %s", c.name, c.command, answer.Stdout)
		case c.want != "" && !strings.Contains(answer.Stdout, c.want):
			t.Errorf("%s: %q was not refused for %q: %s", c.name, c.command, c.want, answer.Stdout)
		}
	}
}
