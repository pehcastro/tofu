package stopcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/ledger"
	"tofu/internal/sys"
)

func TestToLedgerAnswersAndBackCarryTheSameKindAndValue(t *testing.T) {
	in := map[string]jev.Answer{
		"effort":     {Kind: jev.QuestionScore, Score: 1.9},
		"worth_this": {Kind: jev.QuestionNoul, Noul: 0.48},
	}
	converted := toLedgerAnswers(1, in)
	back := toJevAnswers(converted)
	if len(back) != len(in) {
		t.Fatalf("round trip carries %d answers, want %d", len(back), len(in))
	}
	if back["effort"].Score != 1.9 || back["worth_this"].Noul != 0.48 {
		t.Fatalf("round trip = %+v, want the original values", back)
	}
}

type cacheFile struct {
	Questions string          `json:"questions"`
	Answers   []ledger.Answer `json:"answers"`
}

func TestStopCheckCacheOnDiskCarriesNoChoiceAnswer(t *testing.T) {
	dir := sys.RecordedStateDir("cache")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) || dir == "" {
		t.Skipf("%s is not on this machine", dir)
	}
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	total, stopCheck, choice := 0, 0, 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		total++
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		var f cacheFile
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if f.Questions != "stop_check" {
			continue
		}
		stopCheck++
		for _, a := range f.Answers {
			if a.Kind == ledger.AnswerChoice {
				choice++
			}
		}
	}
	t.Logf("%s holds %d stored answers, %d of them under stop_check, %d of those a choice", dir, total, stopCheck, choice)
}
