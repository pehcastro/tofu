package fixture

import (
	"time"

	"tofu/interface/tui/frame"
	"tofu/internal/konst"
)

const (
	Path        = "tofu"
	Branch      = "develop"
	Provider    = "claude-sub"
	Wire        = "anthropic"
	Model       = "claude-opus-5"
	Slug        = Provider + "/" + Model
	SessionName = "amber-cedar-otter"
	SessionID   = "9c4f1a2b-7e3d-4a5c-8b1f-2d6e0a3c5f71"
	Release     = "0.4.0"
	Task        = "why does the gate read the policy first?"
	Width       = 100
	Height      = 30
)

func Opened() time.Time { return time.Date(2026, 9, 19, 14, 32, 0, 0, time.UTC) }

func Context() frame.Context { return frame.Context{Used: 118000, Budget: konst.ContextCeilingTokens} }

func RiskLevels() []string {
	return []string{
		"Read-only or fully reversible inside the workspace: reading or listing files, searching, " +
			"running tests or builds, git status/diff/log, fetching a web page.",
		"Changes the workspace in a way that is easy to undo: editing or creating project files, " +
			"git add/commit/branch/stash, installing project dependencies, running local scripts.",
		"Hard to undo or reaches outside the workspace: deleting files or directories, git push, " +
			"reset --hard, rebase or force-push, editing dotfiles or global config, installing " +
			"system packages, installing or updating third-party agent skills, plugins, extensions, " +
			"hooks or MCP servers, HTTP requests with side effects, sending messages or email, " +
			"running scripts fetched from the internet.",
		"Destructive or irreversible: recursive deletes of important paths, disk, partition or " +
			"filesystem operations, piping a download into a shell, exposing or exfiltrating " +
			"secrets and credentials, production deploys, dropping or migrating shared databases, " +
			"payments, chmod/chown -R on system paths, killing arbitrary processes, sudo or " +
			"privilege escalation.",
	}
}

func Quotas(at time.Time) []frame.Quota {
	return []frame.Quota{
		{Label: Provider + " 5h", Fraction: 0.62, Reported: true, ResetsAt: at.Add(3*time.Hour + 28*time.Minute)},
		{Label: Provider + " weekly", Fraction: 0.31, Reported: true, ResetsAt: at.Add(30 * time.Hour)},
	}
}
