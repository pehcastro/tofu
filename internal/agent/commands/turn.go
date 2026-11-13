package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"slices"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/librarian/skills"
	"tofu/internal/platform/outside"
	"tofu/library"
)

func Expand(typed, root string) (string, error) {
	rest, slashed := strings.CutPrefix(typed, "/")
	name, argument, _ := strings.Cut(rest, " ")
	table := Table()
	at := slices.IndexFunc(table, func(one Command) bool { return one.Name == name && one.Starts() })
	if !slashed || at < 0 {
		return typed, nil
	}
	one := table[at]
	if strings.TrimSpace(argument) == "" && strings.HasPrefix(one.Args, "<") {
		return "", errors.New("/" + name + " needs to know what to work on: type /" + name + " " + one.Args)
	}
	data, err := fs.ReadFile(library.Files(), "dev/skills/"+name+".md")
	if err != nil {
		return "", err
	}
	_, body := skills.FrontMatter(string(data))
	prompt := "The person typed " + typed + ". Follow this skill for it.\n\n" + strings.TrimSpace(body)
	switch one.Needs {
	case Turn:
		return prompt, nil
	case DiffTurn:
		diff, err := lastDiff(root)
		return prompt + "\n\n" + diff, err
	}
	panic("command: " + name + " starts a turn under the need " + string(one.Needs))
}

func lastDiff(root string) (string, error) {
	git := func(args ...string) (string, error) {
		out, err := outside.Command(context.Background(), "git", append([]string{"-C", root}, args...)...).Output()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = errors.New(strings.TrimSpace(string(exit.Stderr)))
		}
		return string(out), err
	}
	head, diff := "The last diff, from git diff HEAD:", ""
	untracked, err := git("ls-files", "--others", "--exclude-standard")
	if err == nil {
		diff, err = git("diff", "HEAD")
	}
	if err == nil && strings.TrimSpace(diff) == "" {
		head = "The tree is clean, so the last diff is the last commit, from git show HEAD:"
		diff, err = git("show", "--format=commit %h %s", "HEAD")
	}
	if err != nil {
		return "", fmt.Errorf("the last diff could not be read in %s: %w", root, err)
	}
	if len(diff) > konst.BashOutputHeldBytes {
		diff = strings.ToValidUTF8(diff[:konst.BashOutputHeldBytes], "") + fmt.Sprintf("\n... cut at %d bytes, as a bash result is; git diff prints the rest", konst.BashOutputHeldBytes)
	}
	block := head + "\n\n```diff\n" + strings.TrimSuffix(diff, "\n") + "\n```"
	if untracked = strings.TrimSpace(untracked); untracked != "" {
		block += "\n\nUntracked, so not in the diff: " + strings.ReplaceAll(untracked, "\n", ", ")
	}
	return block, nil
}
