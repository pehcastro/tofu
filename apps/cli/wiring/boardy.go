package wiring

import (
	"cmp"
	"os"

	"tofu/internal/agent/subagents"
	"tofu/internal/project/boardy"
	settingspkg "tofu/internal/store/settings"
)

const (
	claudeCodeEnv        = "CLAUDECODE"
	claudeCodeSessionEnv = "CLAUDE_CODE_SESSION_ID"
	codexThreadEnv       = "CODEX_THREAD_ID"
)

func BoardyIdentity(as string) (actor, lead string) {
	if session := os.Getenv(subagents.SessionEnv); session != "" {
		if boardy.SubAgentActor(session) {
			return session, ""
		}
		return session, session
	}
	harness, session := boardy.HarnessClaudeCode, os.Getenv(claudeCodeSessionEnv)
	inHarness := session != "" || os.Getenv(claudeCodeEnv) != ""
	if thread := os.Getenv(codexThreadEnv); thread != "" {
		harness, session, inHarness = boardy.HarnessCodex, thread, true
	}
	switch {
	case !inHarness && as == boardy.OrchestratorName:
		return as, as
	case !inHarness:
		return cmp.Or(as, string(boardy.RolePerson)), ""
	case as == "" || as == boardy.OrchestratorName:
		actor = boardy.HarnessActor(harness, session, "")
		return actor, actor
	}
	return boardy.HarnessActor(harness, session, as), ""
}

func ProjectHasBoard(boards boardy.Store, opened error) bool {
	if opened != nil {
		return false
	}
	found, _ := boards.Boards()
	return len(found) > 0
}

func BoardLed(boards boardy.Store, opened error) bool {
	return opened == nil && managementOn(boards.Root)
}

func managementOn(project string) bool {
	store, err := settingspkg.OpenFor(project)
	if err != nil {
		return false
	}
	_, set := store.Source(boardy.ManagementSetting)
	return !set || store.Text(boardy.ManagementSetting) == string(boardy.ManagementBoardy)
}
