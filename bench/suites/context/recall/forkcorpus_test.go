package recall

import (
	"os"
	"strings"
	"testing"

	"tofu/bench/corpus"
)

func TestTheForkCorpusReadsTwentyRealForksFromThreeLineages(t *testing.T) {
	cases, err := ReadForkCorpus()
	if err != nil {
		t.Fatalf("ReadForkCorpus: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("the fork corpus is empty")
	}
	origins := map[string]bool{}
	for _, one := range cases {
		root, _, _ := strings.Cut(one.From, "-f")
		origins[root] = true
	}
	t.Logf("%d real forks read, %d origin lineages, %v", len(cases), len(origins), cases[0].From)
}

func TestEveryForkCorpusRowIsAFixedPointOfTheScrub(t *testing.T) {
	raw, err := os.ReadFile(forkCorpusFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if clean := corpus.Scrub(line); clean != line {
			t.Fatalf("row %d is not a fixed point of the scrub, so it still carries a path or a name from the machine that recorded it", i+1)
		}
	}
}

func TestAPlantedSecretFailsTheCorpusLoad(t *testing.T) {
	planted := `{"from":"turn-planted","into":"turn-planted-f2","step":1,"task":"x","last_word":"the key is sk-ant-api-0123456789abcdef and that is all of it"}`
	if leaks := corpus.LeaksIn(planted); len(leaks) == 0 {
		t.Fatal("a planted credential was not caught by corpus.LeaksIn, so the loader's own check would have shipped it")
	}
	tmp, err := os.CreateTemp(t.TempDir(), "forks-*.jsonl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString(planted + "\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	_ = tmp.Close()

	if _, err := readForkCorpusFile(tmp.Name()); err == nil {
		t.Fatal("a corpus row carrying a planted credential loaded without error")
	}
}
