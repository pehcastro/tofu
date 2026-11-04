package prefix

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"tofu/internal/agent/loop"
	skill "tofu/internal/librarian/skills"
	"tofu/internal/store/session"
	"tofu/internal/tools/registry"
)

const AddedSkill = "bench_added_skill"

const addedSkillFile = "---\nname: " + AddedSkill + "\ndescription: names every file touched in the reply\n---\nname every file you touch.\n"

type ResumeArm struct {
	HeadReadBack bool
}

type ResumedTurn struct {
	ResumeArm
	CacheRead, RequestSize int
	SystemHeld, ToolsHeld  bool
}

func (r ResumedTurn) Share() float64 { return float64(r.CacheRead) / float64(r.RequestSize) }

func MeasureResume(arm ResumeArm) (ResumedTurn, error) {
	measured := ResumedTurn{ResumeArm: arm}
	answering, wire, err := answerDone()
	if err != nil {
		return measured, err
	}
	defer answering.Close()
	scratch, err := os.MkdirTemp("", "bench-resume-")
	if err != nil {
		return measured, err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	shipped, err := RealRules()
	if err != nil {
		return measured, err
	}
	project, store, turns := filepath.Join(scratch, "project"), session.NewStore(filepath.Join(scratch, "sessions")), 0
	config := loop.Config{Model: loop.Subscription{Wire: wire}, Spend: loop.SpendSubscription, Caps: loop.Caps{MaxSteps: 2}, ResultBytesCap: 1 << 16,
		ArtifactDir: filepath.Join(scratch, "artifacts"), Sessions: store, Session: "bench-resume", Inbox: loop.NewInbox(), Prefix: &loop.Prefix{},
		NewID: func() string { turns++; return "turn-bench-" + strconv.Itoa(turns) }}
	for at, task := range []string{RealTask, "now run the test it fixed and say what it printed", "summarise what changed"} {
		switch at {
		case 1:
			folder := filepath.Join(project, ".tofu", "skills", AddedSkill)
			if err := os.MkdirAll(folder, 0o700); err != nil {
				return measured, err
			}
			if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte(addedSkillFile), 0o600); err != nil {
				return measured, err
			}
		case 2:
			config.Inbox, config.Prefix = loop.NewInbox(), &loop.Prefix{}
			if arm.HeadReadBack {
				if config.Prefix, err = loop.HeldPrefix(store, config.Session, ""); err != nil {
					return measured, err
				}
			}
		}
		skills := skill.Discover(project, "").Skills
		config.Tools = loop.NewRegistry()
		if len(skills) > 0 {
			config.Tools = loop.NewRegistry(registry.NewSkill(skills))
		}
		composed, err := loop.Compose(loop.ComposeSpec{Task: task, Environment: laterEnvironment, ToolGuidance: RealToolGuidance, Rules: shipped, Skills: skills})
		if err != nil {
			return measured, err
		}
		config.Task, config.System, config.Parts, config.Environment = task, composed.Head(), composed.Parts, composed.WithTaskRules(laterEnvironment)
		row, err := loop.Run(context.Background(), config)
		if err != nil {
			return measured, err
		}
		config.History = loop.Sendable(row.Conversation)
	}
	bodies := answering.sent()
	if len(bodies) != 3 {
		return measured, fmt.Errorf("three turns sent %d requests, want one each", len(bodies))
	}
	measured.CacheRead, measured.RequestSize = cacheReadBytes(bodies[1], bodies[2])
	measured.SystemHeld, measured.ToolsHeld = systemOf(bodies[0]) == systemOf(bodies[2]), toolsOf(bodies[1]) == toolsOf(bodies[2])
	return measured, nil
}

func toolsOf(body []byte) string {
	var decoded struct {
		Tools json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(body, &decoded)
	return string(decoded.Tools)
}
