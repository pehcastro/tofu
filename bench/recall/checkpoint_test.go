package recall

import (
	"context"
	"strconv"
	"testing"
	"time"

	"tofu/bench/corpus"
	rc "tofu/internal/recall"
	"tofu/internal/sys"
)

const stressForkCeiling = 20000
const realForkCeiling = 50000

type checkpointCase struct {
	name   string
	parent corpus.RecordedTurn
	target int
	isReal bool
}

func readCorpusDirTurn(t *testing.T, id string) corpus.RecordedTurn {
	t.Helper()
	turn, err := corpus.ReadTurnDir(sys.RecordedStateDir("sessions", id))
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return turn
}

func readCorpusFileTurn(t *testing.T, id string) corpus.RecordedTurn {
	t.Helper()
	turn, err := corpus.ReadTurn(sys.RecordedStateDir("sessions", id+".json"))
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return turn
}

func checkpointCases(t *testing.T) []checkpointCase {
	t.Helper()
	return []checkpointCase{
		{
			name:   "turn-18d6f8d9e45f8efc, real work, \"did the benchmark had any results?\"",
			parent: readCorpusDirTurn(t, "turn-18d6f8d9e45f8efc"),
			target: realForkCeiling,
			isReal: true,
		},
		{
			name:   "turn-18d6d295dfac466c, recorded stress task at a lowered ceiling",
			parent: readCorpusFileTurn(t, "turn-18d6d295dfac466c"),
			target: stressForkCeiling,
			isReal: false,
		},
	}
}

func caseBands(t *testing.T, target int) rc.Bands {
	t.Helper()
	t.Setenv(rc.CeilingVariable, strconv.Itoa(target))
	budget, err := rc.BudgetFor(replayedModel, replayedModelWindow)
	if err != nil {
		t.Fatalf("budget at %d: %v", target, err)
	}
	return budget.Bands
}

type builtArms struct {
	freshChild        string
	replayed          string
	freshBuildMicros  int64
	replayBuildMicros int64
}

func buildBothArms(t *testing.T, cfg rc.Config, bands rc.Bands, one checkpointCase) builtArms {
	t.Helper()
	session := CorpusSession(one.parent)
	result := replay(t, cfg, bands, session, armDistilled)
	carry, forked := FreshChildCarry(result)
	if !forked {
		t.Fatalf("%s never crossed its %d token target, so the current mechanism never forked it", one.name, bands.Target())
	}
	started := time.Now()
	replayed := RawReplayText(one.parent)
	return builtArms{
		freshChild:        carry.Text,
		replayed:          replayed,
		freshBuildMicros:  result.Forks[len(result.Forks)-1].BlockedMicros,
		replayBuildMicros: time.Since(started).Microseconds(),
	}
}

func TestFreshChildAndFullReplayPastedInFullForTwoRealForks(t *testing.T) {
	cfg, err := rc.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, one := range checkpointCases(t) {
		bands := caseBands(t, one.target)
		arms := buildBothArms(t, cfg, bands, one)
		freshTokens, replayTokens := cfg.Tokens(arms.freshChild), cfg.Tokens(arms.replayed)
		freshMatched, keywords := KeywordOverlap(one.parent.Task, arms.freshChild)
		replayMatched, _ := KeywordOverlap(one.parent.Task, arms.replayed)
		t.Logf("case: %s\nreal recorded fork: %v\n\n--- fresh child, current facts band, %d tokens, built in %d microseconds ---\n%s\n\n--- replayed abandoned branch, full transcript, %d tokens, built in %d microseconds ---\n%s\n\nfree arm, task keyword overlap: fresh child %d/%d, replay %d/%d",
			one.name, one.isReal,
			freshTokens, arms.freshBuildMicros, arms.freshChild,
			replayTokens, arms.replayBuildMicros, arms.replayed,
			freshMatched, keywords, replayMatched, keywords)
	}
}

func TestJevScoresBothArmsOnBothRealForks(t *testing.T) {
	wire := liveJevWire(t)
	cfg, err := rc.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	for _, one := range checkpointCases(t) {
		bands := caseBands(t, one.target)
		arms := buildBothArms(t, cfg, bands, one)

		freshScore, err := JevAnswerability(context.Background(), wire, one.parent.Task, "fresh_child", arms.freshChild)
		if err != nil {
			t.Fatalf("%s: fresh child score: %v", one.name, err)
		}
		replayScore, err := JevAnswerability(context.Background(), wire, one.parent.Task, "replayed_branch", arms.replayed)
		if err != nil {
			t.Fatalf("%s: replayed branch score: %v", one.name, err)
		}
		t.Logf("case: %s\nfresh child   : score %.2f, confidence %.2f, build %s, %d ms, $%.6f\nreplayed branch: score %.2f, confidence %.2f, build %s, %d ms, $%.6f",
			one.name,
			freshScore.Score, freshScore.Confidence, freshScore.Build, freshScore.LatencyMS, freshScore.CostUSD,
			replayScore.Score, replayScore.Confidence, replayScore.Build, replayScore.LatencyMS, replayScore.CostUSD)
	}
}
