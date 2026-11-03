package judge

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/question"
	"tofu/internal/models/classifiers"
	"tofu/internal/sys"
	"tofu/internal/turn"
	"tofu/library/questions"
)

func TestNoTaskSharesARunOfWordsWithTheKindQuestion(t *testing.T) {
	set, _, err := question.Resolve("task_kind@1", []question.Layer{{Name: "library", Origin: "library/questions", FS: questions.Files()}})
	if err != nil {
		t.Fatal(err)
	}
	asked, _ := set.Question("kind")
	wording := asked.Instructions
	for _, option := range asked.ToJev().Options {
		wording += " " + fmt.Sprint(option.Criteria)
	}
	words := func(text string) []string {
		return strings.Fields(strings.ToLower(strings.NewReplacer(",", "", ".", "", ":", "").Replace(text)))
	}
	joined := " " + strings.Join(words(wording), " ") + " "
	shared := func(task string) string {
		said := words(task)
		for at := 0; at+leakWords <= len(said); at++ {
			if run := strings.Join(said[at:at+leakWords], " "); strings.Contains(joined, " "+run+" ") {
				return run
			}
		}
		return ""
	}
	if planted := "tell me what is wrong with it"; shared(planted) == "" {
		t.Fatalf("a task copied from the question's own wording is not caught, so this check proves nothing: %q", planted)
	}
	for _, c := range KindCorpus() {
		if run := shared(c.Task); run != "" {
			t.Errorf("%q shares %q with the question, which answers it for free", c.Task, run)
		}
	}
}

func TestTaskKindAgainstTheWordList(t *testing.T) {
	if os.Getenv("TOFU_LIVE") != "1" {
		t.Skip("set TOFU_LIVE=1, with a home under the temp dir whose classifier is bound, to put the question to the real jev route")
	}
	sys.AllowLiveCredential(t)
	project := sys.SourceRoot()
	if _, err := classifier.Open(project, jev.ReadKeyring(), 1); err != nil {
		t.Fatalf("TOFU_LIVE=1 and jev cannot be asked, so nothing is measured: %v", err)
	}
	judge := turn.NewKindJudge(project)
	cases := len(KindCorpus())
	for pass := 1; pass <= 2; pass++ {
		jev, words, fellBack := KindAgainstTheWordList(judge)
		t.Logf("pass %d: jev %d of %d, word list %d of %d, fell back %d", pass, jev.Right, cases, words.Right, cases, len(fellBack))
		for _, one := range jev.Missed {
			t.Logf("pass %d jev missed %s", pass, one)
		}
		for _, one := range fellBack {
			t.Errorf("pass %d fell back to the word list on %s", pass, one)
		}
		if pass == 1 {
			for _, one := range words.Missed {
				t.Logf("word list missed %s", one)
			}
		}
	}
}
