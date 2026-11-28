package shells

import (
	"context"
	"encoding/json"
	"errors"

	"tofu/internal/agent/cron"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	settingspkg "tofu/internal/store/settings"
)

func CronChecker(dir string) cron.Checker {
	return func(ctx context.Context, command string) (int, string, error) {
		shell, err := loop.ResolveRunShell(ctx, settingspkg.TextSaying(dir, settingspkg.Shell, nil))
		if err != nil {
			return 0, "", err
		}
		bash, err := loop.NewBashToolFromShell(dir, shell)
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
