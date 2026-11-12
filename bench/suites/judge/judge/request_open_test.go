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
	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
	"tofu/library/questions"
)

const leakWords = 4

func shippedOpenQuestion(t *testing.T, ref string) OpenQuestion {
	t.Helper()
	asked, err := OpenQuestionIn(ref, []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
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
	asked := shippedOpenQuestion(t, RequestOpen)
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
		for _, text := range []string{c.Said, c.Replied, c.Task} {
			if run := shared(text); run != "" {
				t.Errorf("%q shares %q with the question, which answers it for free", text, run)
			}
		}
	}
}

type arm struct {
	name       string
	right      int
	restatedOf map[string]int
	of         map[string]int
}

func newArm(name string) arm {
	return arm{name: name, restatedOf: map[string]int{}, of: map[string]int{}}
}

func (a *arm) count(c Case, open bool) {
	a.of[c.Kind]++
	if open {
		a.restatedOf[c.Kind]++
	}
	if open == c.Open {
		a.right++
	}
}

func (a arm) byKind() string {
	var kinds []string
	for _, kind := range slices.Sorted(maps.Keys(a.of)) {
		kinds = append(kinds, kind+" restated "+strconv.Itoa(a.restatedOf[kind])+" of "+strconv.Itoa(a.of[kind]))
	}
	return strings.Join(kinds, ", ")
}

func TestRequestOpenAgainstAnArmThatAlwaysRestates(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	credential.AllowLiveCredential(t)
	key, err := core.TypeSafeKey(filepath.Join(fs.SourceRoot(), ".env"))
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
	cases := corpus(t)
	always := newArm("always restate")
	for _, c := range cases {
		always.count(c, true)
	}
	t.Logf("%s: right %d of %d, %s", always.name, always.right, len(cases), always.byKind())
	for _, ref := range []string{RequestOpenBefore, RequestOpen} {
		asked := shippedOpenQuestion(t, ref)
		for pass := 1; pass <= 2; pass++ {
			judged := newArm(ref + " on " + strings.Join(asked.State, "+") + " at " + strconv.FormatFloat(asked.Threshold, 'f', -1, 64))
			var latencies []time.Duration
			var cost float64
			builds, failed := map[string]bool{}, 0
			for _, c := range cases {
				got, err := asked.Ask(context.Background(), client, c)
				if err != nil {
					failed++
					t.Logf("%s pass %d failed on %q: %v", ref, pass, c.Said, err)
					continue
				}
				judged.count(c, got.Score >= asked.Threshold)
				latencies, cost, builds[got.Build] = append(latencies, got.Latency), cost+got.Cost, true
				if (got.Score >= asked.Threshold) != c.Open {
					t.Logf("%s pass %d wrong: %.2f on %q, labelled open=%v (%s, %s)", ref, pass, got.Score, c.Said, c.Open, c.Kind, c.Shape)
				}
			}
			slices.Sort(latencies)
			t.Logf("%s pass %d: %d cases, %d failed, builds %v, cost USD %.6f", ref, pass, len(cases), failed, slices.Sorted(maps.Keys(builds)), cost)
			if len(latencies) > 0 {
				t.Logf("%s pass %d latency: p50 %s, p99 %s", ref, pass, latencies[len(latencies)/2].Round(time.Millisecond), latencies[len(latencies)*99/100].Round(time.Millisecond))
			}
			t.Logf("%s: right %d of %d, %s", judged.name, judged.right, len(cases), judged.byKind())
		}
	}
}
