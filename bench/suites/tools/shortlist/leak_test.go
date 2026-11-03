package shortlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCorpusRefusesARowThatCameOffTheRecordingMachine(t *testing.T) {
	clean, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"a home path off the recording machine": filepath.Join(home, ".local", "bin"),
		"something shaped like a credential":    "sk-ant-oat" + "-not-a-real-token",
	}
	for name, planted := range cases {
		t.Run(name, func(t *testing.T) {
			row, err := json.Marshal(Row{
				TurnID: "planted",
				Task:   "t",
				Files:  []File{{Path: "x", Content: planted}},
				Label:  []string{"x"},
			})
			if err != nil {
				t.Fatal(err)
			}
			copyPath := filepath.Join(t.TempDir(), "shortlist-corpus.jsonl")
			if err := os.WriteFile(copyPath, append(append([]byte{}, clean...), append(row, '\n')...), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = readCorpusFile(copyPath)
			if err == nil {
				t.Fatal("readCorpusFile accepted a row carrying what the scrub removes")
			}
			if !strings.Contains(err.Error(), "came off the recording machine") {
				t.Fatalf("the refusal does not say what is wrong: %v", err)
			}
		})
	}
}
