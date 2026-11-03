package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"tofu/internal/cli/output"
	"tofu/internal/project/boardy"
	"tofu/internal/project/guard"
	"tofu/internal/store/settings"
)

const guardUsage = "usage: tofu guard <rule> < hook.json, or tofu guard git commit-msg|release-docs <message file>"

func guardVerb(args []string, in io.Reader, out, errOut io.Writer) int {
	answer, err := guardAnswer(args, in)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, "tofu guard: "+err.Error())
		_, _ = fmt.Fprintln(errOut, guardUsage)
		return output.ExitUsage
	}
	_, _ = fmt.Fprint(out, answer.Stdout)
	_, _ = fmt.Fprint(errOut, answer.Stderr)
	return answer.Code
}

func guardAnswer(args []string, in io.Reader) (guard.Answer, error) {
	wd, err := os.Getwd()
	switch {
	case err != nil:
		return guard.Answer{}, err
	case len(args) == 3 && args[0] == "git" && args[1] == "commit-msg":
		return guard.CommitMsg(wd, args[2]), nil
	case len(args) == 3 && args[0] == "git" && args[1] == "release-docs":
		return guard.ReleaseDocs(wd, args[2]), nil
	case len(args) != 1:
		return guard.Answer{}, fmt.Errorf("one rule: %v, or git commit-msg|release-docs <file>", guard.Rules())
	}
	stdin, err := io.ReadAll(in)
	if err != nil {
		return guard.Answer{}, err
	}
	root := os.Getenv("CLAUDE_PROJECT_DIR")
	if strings.TrimSpace(root) == "" {
		root = wd
	}
	cache, _ := os.UserCacheDir()
	env := guard.Env{
		Root:   root,
		Temp:   os.TempDir(),
		Cache:  cache,
		Boardy: settings.TextSaying(root, boardy.ManagementSetting, nil) == string(boardy.ManagementBoardy),
	}
	return guard.Hook(guard.Rule(args[0]), env, stdin)
}
