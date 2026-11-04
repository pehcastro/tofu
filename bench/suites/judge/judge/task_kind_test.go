package judge

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"tofu/internal/agent/loop"
	question "tofu/internal/judge/questions"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/credential"
	"tofu/internal/platform/fs"
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
	credential.AllowLiveCredential(t)
	project := fs.SourceRoot()
	if _, err := classifiers.Open(project, core.ReadKeyring(), 1); err != nil {
		t.Fatalf("TOFU_LIVE=1 and jev cannot be asked, so nothing is measured: %v", err)
	}
	judge := loop.NewKindJudge(project)
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
