package guard

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellHasNoSideDoors(t *testing.T) {
	cases := []struct {
		name, agent, command, want string
	}{
		{name: "plain commit from a subagent", agent: "a1", command: "git commit -m x", want: "Git may not change"},
		{name: "git -c before commit", agent: "a1", command: "git -c user.name=x commit -m x", want: "Git may not change"},
		{name: "git -c with a quoted value", agent: "a1", command: `git -c "user.name=A B" commit -m x`, want: "Git may not change"},
		{name: "git -C before commit", agent: "a1", command: "git -C /tmp/x commit -m x", want: "Git may not change"},
		{name: "git-dir and work-tree before add", agent: "a1", command: "git --git-dir=.git --work-tree=. add -A", want: "Git may not change"},
		{name: "git -C inside bash -c", agent: "a1", command: `bash -c "git -C x commit -m y"`, want: "Git may not change"},
		{name: "read-only git from a subagent", agent: "a1", command: "git -C x status"},
		{name: "the commit-msg hook named in a command", agent: "a1", command: "tofu guard git commit-msg .git/COMMIT_EDITMSG"},
		{name: "the commit-msg hook named in text", agent: "a1", command: `echo "the git commit-msg hook refuses it"`},
		{name: "git -C reset --hard from the orchestrator", command: "git -C x reset --hard", want: "discards uncommitted work"},
		{name: "git -c before stash", command: "git -c a=b stash", want: "discards uncommitted work"},
		{name: "git -C before stash list", command: "git -C x stash list", want: ""},
		{name: "git -C before worktree", command: "git -C x worktree add y", want: "git worktree is refused"},
		{name: "plain sed on a go file", command: "sed -i 's/a/b/' x.go", want: "Writing a source file through the shell"},
		{name: "sed on a variable path", command: `sed -i 's/a/b/' "$f"`, want: "Writing a source file through the shell"},
		{name: "sed on a variable in a loop", command: "for f in *.go; do sed -i 's/a/b/' $f; done", want: "Writing a source file through the shell"},
		{name: "sed with a suffix on a braced variable", command: "sed -i.bak -e 's/a/b/' ${file}", want: "Writing a source file through the shell"},
		{name: "sed --in-place on a variable", command: `sed --in-place 's/a/b/' "$f"`, want: "Writing a source file through the shell"},
		{name: "sed on a command substitution", command: "sed -i 's/a/b/' $(ls)", want: "Writing a source file through the shell"},
		{name: "sed from find -exec", command: `find . -name '*.txt' -exec sed -i 's/a/b/' {} \;`, want: "Writing a source file through the shell"},
		{name: "sed from xargs", command: "ls | xargs sed -i 's/a/b/'", want: "Writing a source file through the shell"},
		{name: "sed on a variable inside bash -c", command: `bash -c 'sed -i s/a/b/ $f'`, want: "Writing a source file through the shell"},
		{name: "plain sed on a markdown file", command: "sed -i 's/a/b/' notes.md"},
		{name: "sed deleting the last line of a markdown file", command: "sed -i '$d' notes.md"},
		{name: "sed in place with the BSD empty suffix", command: "sed -i '' 's/a/b/' notes.md"},
		{name: "sed printing from a variable", command: `sed -n 's/a/b/p' "$f"`},
		{name: "grep for the text sed -i", command: `grep -n "sed -i" notes.md`},
	}
	for _, c := range cases {
		stdin, _ := json.Marshal(map[string]any{"tool_name": "Bash", "agent_id": c.agent, "tool_input": map[string]string{"command": c.command}})
		answer, err := Hook(Shell, Env{Root: t.TempDir(), Temp: t.TempDir()}, stdin)
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

func TestCommitMsgRefusesAMissingMessageFile(t *testing.T) {
	if answer := CommitMsg(t.TempDir(), filepath.Join(t.TempDir(), "COMMIT_EDITMSG")); answer.Code == 0 {
		t.Fatalf("a missing message file was allowed: %+v", answer)
	}
}
