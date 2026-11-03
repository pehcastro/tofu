package standing

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"tofu/bench/api"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/recall"
	"tofu/internal/sys"
	"tofu/library/questions"
)

const (
	leakWords  = 4
	corpusSize = 40
)

func shippedQuestion(t *testing.T) Question {
	t.Helper()
	asked, err := QuestionIn([]question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	return asked
}

func corpus(t *testing.T) []Case {
	t.Helper()
	cases, err := Cases(filepath.Join("testdata", "standing.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	orders := 0
	for _, c := range cases {
		if c.Standing {
			orders++
		}
	}
	if len(cases) != corpusSize || orders != corpusSize/2 {
		t.Fatalf("the corpus holds %d sentences and %d orders, want %d and %d", len(cases), orders, corpusSize, corpusSize/2)
	}
	return cases
}

func TestNoSentenceSharesARunOfWordsWithTheQuestion(t *testing.T) {
	asked := shippedQuestion(t)
	wording := " " + strings.Join(strings.Fields(strings.ToLower(asked.Asked.Instructions+" "+asked.Asked.True+" "+asked.Asked.False)), " ") + " "
	shared := func(sentence string) string {
		said := strings.Fields(strings.ToLower(strings.Trim(sentence, ".?!")))
		for at := 0; at+leakWords <= len(said); at++ {
			if run := strings.Join(said[at:at+leakWords], " "); strings.Contains(wording, " "+run+" ") {
				return run
			}
		}
		return ""
	}
	if planted := "it would still bind the agent tomorrow"; shared(planted) == "" {
		t.Fatalf("a sentence copied from the question's own wording is not caught, so this check proves nothing: %q", planted)
	}
	for _, c := range corpus(t) {
		if run := shared(c.Sentence); run != "" {
			t.Errorf("%q shares %q with the question, which answers it for free", c.Sentence, run)
		}
	}
}

type arm struct {
	name                  string
	kept, wrongKept, lost int
}

func (a *arm) count(c Case, kept bool) {
	switch {
	case kept && c.Standing:
		a.kept++
	case kept:
		a.wrongKept++
	case c.Standing:
		a.lost++
	}
}

func (a arm) line() string {
	precision, recall := 0.0, 0.0
	if a.kept+a.wrongKept > 0 {
		precision = float64(a.kept) / float64(a.kept+a.wrongKept)
	}
	if a.kept+a.lost > 0 {
		recall = float64(a.kept) / float64(a.kept+a.lost)
	}
	return fmt.Sprintf("%s: precision %.3f (%d of %d kept are orders), recall %.3f (%d of %d orders kept)", a.name, precision, a.kept, a.kept+a.wrongKept, recall, a.kept, a.kept+a.lost)
}

func wordList(t *testing.T, cases []Case) arm {
	words := arm{name: "word list"}
	for _, c := range cases {
		kept := recall.StandingByWords(c.Sentence)
		words.count(c, kept)
		if kept != c.Standing {
			t.Logf("word list wrong on %q, labelled standing=%v (%s)", c.Sentence, c.Standing, c.Shape)
		}
	}
	return words
}

func TestTheWordListArm(t *testing.T) {
	t.Log(wordList(t, corpus(t)).line())
}

func TestJevAgainstTheWordList(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1 to put the question to the real jev route")
	}
	sys.AllowLiveCredential(t)
	key, err := jev.TypeSafeKey(filepath.Join(sys.SourceRoot(), ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	wire, err := api.NewWire(key)
	if err != nil {
		t.Fatal(err)
	}
	client, err := jev.NewClient(jev.Config{Wire: wire})
	if err != nil {
		t.Fatal(err)
	}
	asked, cases := shippedQuestion(t), corpus(t)
	words := wordList(t, cases)
	for pass := 1; pass <= 2; pass++ {
		judged := arm{name: "jev at standing_at " + strconv.FormatFloat(asked.Threshold, 'f', -1, 64)}
		var latencies []time.Duration
		var cost float64
		builds, failed := map[string]bool{}, 0
		for _, c := range cases {
			got, err := asked.Ask(context.Background(), client, c.Sentence)
			if err != nil {
				failed++
				judged.count(c, recall.StandingByWords(c.Sentence))
				t.Logf("pass %d failed on %q, the word list decides: %v", pass, c.Sentence, err)
				continue
			}
			judged.count(c, got.Score >= asked.Threshold)
			latencies, cost, builds[got.Build] = append(latencies, got.Latency), cost+got.Cost, true
			if (got.Score >= asked.Threshold) != c.Standing {
				t.Logf("pass %d wrong: %.2f on %q, labelled standing=%v (%s)", pass, got.Score, c.Sentence, c.Standing, c.Shape)
			}
		}
		slices.Sort(latencies)
		t.Logf("pass %d: %d cases, %d failed, builds %v, cost USD %.6f", pass, len(cases), failed, slices.Sorted(maps.Keys(builds)), cost)
		if len(latencies) > 0 {
			t.Logf("pass %d latency: p50 %s, p99 %s", pass, latencies[len(latencies)/2].Round(time.Millisecond), latencies[len(latencies)*99/100].Round(time.Millisecond))
		}
		t.Logf("pass %d %s", pass, judged.line())
		t.Logf("pass %d %s", pass, words.line())
	}
}
