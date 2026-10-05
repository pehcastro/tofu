package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"

	"tofu/internal/cron"
	"tofu/internal/konst"
	sessionstore "tofu/internal/session"
	settingspkg "tofu/internal/settings"
	"tofu/internal/turn"
)

var gitPush = regexp.MustCompile(`\bgit\b[^;&|\n]*\bpush\b`)

func cronFile(sessions *sessionstore.Store, id string) string {
	return filepath.Join(sessions.Dir(id), "cron.json")
}

func cronChecker(dir string) cron.Checker {
	return func(ctx context.Context, command string) (int, string, error) {
		shell, err := turn.ResolveRunShell(settingText(dir, settingspkg.Shell, nil))
		if err != nil {
			return 0, "", err
		}
		bash, err := turn.NewBashToolFromShell(dir, shell)
		if err != nil {
			return 0, "", err
		}
		args, _ := json.Marshal(map[string]any{"command": command, "timeout_ms": konst.CronCheckTimeoutMillis})
		result, err := bash.Run(ctx, args)
		switch {
		case err != nil:
			return 0, "", err
		case result.ExitCode == nil:
			return 0, "", errors.New(result.FailureText)
		}
		return *result.ExitCode, result.Content, nil
	}
}

type firedBash struct{ turn.Tool }

func (f firedBash) Run(ctx context.Context, raw json.RawMessage) (turn.Result, error) {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &args) == nil && gitPush.MatchString(args.Command) {
		return turn.Result{}, errors.New("bash: a turn a cron job started never pushes, whatever the gate says: leave the push to the person")
	}
	return f.Tool.Run(ctx, raw)
}

func withoutPush(built []turn.Tool) []turn.Tool {
	guarded := make([]turn.Tool, len(built))
	for index, tool := range built {
		guarded[index] = tool
		if tool.Name() == "bash" {
			guarded[index] = firedBash{tool}
		}
	}
	return guarded
}
