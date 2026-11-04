package judge

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/classifiers/providers/typesafe"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const leakWords = 4

func shippedOpenQuestion(t *testing.T) OpenQuestion {
	t.Helper()
	asked, err := OpenQuestionIn([]question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	return asked
}

func corpus(t *testing.T) []Case {
	t.Helper()
	cases, err := Cases(filepath.Join("testdata", "request_open.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestNoRequestSharesARunOfWordsWithTheQuestion(t *testing.T) {
	asked := shippedOpenQuestion(t)
	wording := " " + strings.Join(strings.Fields(strings.ToLower(asked.Asked.Instructions+" "+asked.Asked.True+" "+asked.Asked.False)), " ") + " "
	shared := func(request string) string {
		said := strings.Fields(strings.ToLower(strings.Trim(request, ".?!")))
		for at := 0; at+leakWords <= len(said); at++ {
			if run := strings.Join(said[at:at+leakWords], " "); strings.Contains(wording, " "+run+" ") {
				return run
			}
		}
		return ""
	}
	if planted := "please leave to the agent which change makes it true"; shared(planted) == "" {
		t.Fatalf("a request copied from the question's own wording is not caught, so this check proves nothing: %q", planted)
	}
	for _, c := range corpus(t) {
		if run := shared(c.Said); run != "" {
			t.Errorf("%q shares %q with the question, which answers it for free", c.Said, run)
		}
	}
}

type arm struct {
	name                    string
	right, restated, missed int
}

func (a *arm) count(c Case, open bool) {
	switch {
	case open == c.Open:
		a.right++
	case open:
		a.restated++
	default:
		a.missed++
	}
}

func TestRequestOpenAgainstAnArmThatAlwaysRestates(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	sys.AllowLiveCredential(t)
	key, err := core.TypeSafeKey(filepath.Join(sys.SourceRoot(), ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := typesafe.NewWire(key)
	if err != nil {
		t.Fatal(err)
	}
	client, err := core.NewClient(core.Config{Wire: wire})
	if err != nil {
		t.Fatal(err)
	}
	asked, cases := shippedOpenQuestion(t), corpus(t)
	always := arm{name: "always restate"}
	for _, c := range cases {
		always.count(c, true)
	}
	for pass := 1; pass <= 2; pass++ {
		judged := arm{name: "jev at open_at " + strconv.FormatFloat(asked.Threshold, 'f', -1, 64)}
		var latencies []time.Duration
		var cost float64
		builds, failed := map[string]bool{}, 0
		for _, c := range cases {
			got, err := asked.Ask(context.Background(), client, c.Said)
			if err != nil {
				failed++
				t.Logf("pass %d failed on %q: %v", pass, c.Said, err)
				continue
			}
			judged.count(c, got.Score >= asked.Threshold)
			latencies, cost, builds[got.Build] = append(latencies, got.Latency), cost+got.Cost, true
			if (got.Score >= asked.Threshold) != c.Open {
				t.Logf("pass %d wrong: %.2f on %q, labelled open=%v (%s)", pass, got.Score, c.Said, c.Open, c.Shape)
			}
		}
		slices.Sort(latencies)
		t.Logf("pass %d: %d cases, %d failed, builds %v, cost USD %.6f", pass, len(cases), failed, slices.Sorted(maps.Keys(builds)), cost)
		if len(latencies) > 0 {
			t.Logf("pass %d latency: p50 %s, p99 %s", pass, latencies[len(latencies)/2].Round(time.Millisecond), latencies[len(latencies)*99/100].Round(time.Millisecond))
		}
		for _, a := range []arm{judged, always} {
			t.Logf("pass %d %-22s right %d of %d (%.1f%%), closed restated %d, open missed %d", pass, a.name, a.right, len(cases), 100*float64(a.right)/float64(len(cases)), a.restated, a.missed)
		}
	}
}
