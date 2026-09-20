package readworth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadCorpusRefusesARowThatCameOffTheRecordingMachine(t *testing.T) {
	clean, err := os.ReadFile(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(corpusFile, clean, 0o600); err != nil {
			t.Fatal(err)
		}
	})

	cases := map[string]string{
		"a home path off the recording machine": "C:\\Users\\Luiz\\.local\\bin",
		"something shaped like a credential":    "sk-ant-oat" + "-not-a-real-token",
	}
	for name, planted := range cases {
		t.Run(name, func(t *testing.T) {
			row, err := json.Marshal(Row{Turn: "planted", Task: "t", Position: "opening", Paragraph: planted, Keep: true})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(corpusFile, append(clean, append(row, '\n')...), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = ReadCorpus()
			if err == nil {
				t.Fatal("ReadCorpus accepted a row carrying what the scrub removes")
			}
			if !strings.Contains(err.Error(), "came off the recording machine") {
				t.Fatalf("the refusal does not say what is wrong: %v", err)
			}
		})
	}
}

func TestTheShippedCorpusHoldsAtLeastFiftyLabelledParagraphs(t *testing.T) {
	rows, err := ReadCorpus()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 50 {
		t.Fatalf("the corpus holds %d rows, want at least 50", len(rows))
	}
	kept, dropped := 0, 0
	for _, row := range rows {
		if row.Keep {
			kept++
			continue
		}
		dropped++
	}
	t.Logf("%d rows, %d labelled keep, %d labelled drop", len(rows), kept, dropped)
}
