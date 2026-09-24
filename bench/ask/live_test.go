package ask

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"tofu/bench/api"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/subagent"
	library "tofu/library"
	libraryquestions "tofu/library/questions"
)

const repoRoot = "../.."

func liveClient(t *testing.T) (*jev.Client, question.Set, subagent.AskRule) {
	t.Helper()
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	jev.AllowLiveCredential(t)
	key, err := jev.Key(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := api.NewWire(key)
	if err != nil {
		t.Fatalf("NewWire: %v", err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	layers := []question.Layer{{Name: "library", Origin: "library/questions", FS: libraryquestions.Files()}}
	set, _, err := question.Resolve(Point, layers)
	if err != nil {
		t.Fatalf("resolve %s: %v", Point, err)
	}
	pol, err := subagent.LoadAskRule(library.Files(), "ask@1")
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	return client, set, pol
}

func TestTheSevenCountedMomentsJudgedAgainstTheFreeArm(t *testing.T) {
	client, set, pol := liveClient(t)
	corpus := Corpus()

	agreeJudged, agreeFree := 0, 0
	var latencies []time.Duration
	cost := 0.0
	for _, m := range corpus {
		judged, err := Ask(context.Background(), client, set, m.State)
		if err != nil {
			t.Fatalf("%s %s: Ask: %v", m.Ticket, m.Cite, err)
		}
		verdict, err := subagent.DecideAsk(judged.Action, judged.Determined, pol.DeterminedLowAt)
		if err != nil {
			t.Fatalf("%s %s: DecideAsk: %v", m.Ticket, m.Cite, err)
		}
		free := Free(m.State)

		latencies = append(latencies, judged.Latency)
		cost += judged.Cost
		if verdict.Effective == m.Ideal {
			agreeJudged++
		}
		if free == m.Ideal {
			agreeFree++
		}
		t.Logf("%-8s %-28s label=%-8s ideal=%-12s free=%-12s judged action=%-12s determined=%.2f effective=%-12s latency=%s cost=$%.6f",
			m.Ticket, m.Cite, m.Label, m.Ideal, free, judged.Action, judged.Determined, verdict.Effective, judged.Latency.Round(time.Millisecond), judged.Cost)
	}

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var sum time.Duration
	for _, l := range latencies {
		sum += l
	}
	t.Logf("judged arm: %d of %d agree with the ideal label, free arm: %d of %d, %d calls, $%.6f total, $%.6f per decision, mean %s, p50 %s",
		agreeJudged, len(corpus), agreeFree, len(corpus), len(latencies), cost, cost/float64(len(latencies)),
		(sum / time.Duration(len(latencies))).Round(time.Millisecond), latencies[len(latencies)/2].Round(time.Millisecond))
}

func TestTheSameMomentJudgedTwice(t *testing.T) {
	client, set, _ := liveClient(t)
	state := Corpus()[2].State

	first, err := Ask(context.Background(), client, set, state)
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	second, err := Ask(context.Background(), client, set, state)
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	spread := first.Determined - second.Determined
	if spread < 0 {
		spread = -spread
	}
	t.Logf("BOJI-015:383 judged twice: determined %.4f then %.4f, spread %.4f; action %q then %q",
		first.Determined, second.Determined, spread, first.Action, second.Action)
}

func TestTheSameMomentUnderTwoTickets(t *testing.T) {
	client, set, _ := liveClient(t)
	state := Corpus()[5].State

	under := func(ticket string) Judged {
		judged, err := Ask(context.Background(), client, set, state)
		if err != nil {
			t.Fatalf("Ask under %s: %v", ticket, err)
		}
		return judged
	}
	first := under("BOJI-011")
	second := under("BOJI-999")

	spread := first.Determined - second.Determined
	if spread < 0 {
		spread = -spread
	}
	t.Logf("cmd/boji/bench.go under BOJI-011 and under BOJI-999: determined %.4f then %.4f, spread %.4f; action %q then %q. "+
		"the ticket id is never part of the state sent to jev, so nothing here can move it",
		first.Determined, second.Determined, spread, first.Action, second.Action)
}
