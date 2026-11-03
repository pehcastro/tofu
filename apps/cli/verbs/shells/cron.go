package shells

import (
	"context"
	"encoding/json"
	"errors"

	"tofu/internal/agent/cron"
	"tofu/internal/agent/loop"
	"tofu/internal/konst"
	settingspkg "tofu/internal/store/settings"
	"tofu/internal/sys"
	"tofu/internal/tools/contract"
)

var gitPush = sys.LazyRegexp(`\bgit\b[^;&|\n]*\bpush\b`)

func CronChecker(dir string) cron.Checker {
	return func(ctx context.Context, command string) (int, string, error) {
		shell, err := turn.ResolveRunShell(settingspkg.TextSaying(dir, settingspkg.Shell, nil))
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

type firedBash struct{ contract.Tool }

func (f firedBash) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Command string `json:"command"`
	}
	if json.Unmarshal(raw, &args) == nil && gitPush().MatchString(args.Command) {
		return contract.Result{}, errors.New("bash: a turn a cron job started never pushes, whatever the gate says: leave the push to the person")
	}
	return f.Tool.Run(ctx, raw)
}

func WithoutPush(built []contract.Tool) []contract.Tool {
	guarded := make([]contract.Tool, len(built))
	for index, tool := range built {
		guarded[index] = tool
		if tool.Name() == "bash" {
			guarded[index] = firedBash{tool}
		}
	}
	return guarded
}
