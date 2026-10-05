package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/turn"
)

func TestEditKeepsTheLineEndingsAndBOMOfTheFile(t *testing.T) {
	const bom = "\xef\xbb\xbf"
	for _, row := range []struct {
		name, file string
		edits      [][2]string
		want       string
	}{
		{"an LF old_string on a CRLF file, then a second edit", "one\r\ntwo\r\nthree\r\nfour\r\n",
			[][2]string{{"two\nthree", "TWO\nTHREE"}, {"four\n", "FOUR\nFIVE\n"}}, "one\r\nTWO\r\nTHREE\r\nFOUR\r\nFIVE\r\n"},
		{"a CRLF new_string on a CRLF file", "one\r\ntwo\r\n",
			[][2]string{{"one\r\n", "ONE\r\nHALF\r\n"}}, "ONE\r\nHALF\r\ntwo\r\n"},
		{"a BOM file with old_string on line one", bom + "one\r\ntwo\r\n",
			[][2]string{{"one\ntwo", "1\n2"}}, bom + "1\r\n2\r\n"},
		{"an old_string carrying the BOM", bom + "one\ntwo\n",
			[][2]string{{bom + "one", "1"}}, bom + "1\ntwo\n"},
		{"a mixed file keeps the lines the edit did not touch", "one\r\ntwo\nthree\r\n",
			[][2]string{{"two", "TWO"}}, "one\r\nTWO\nthree\r\n"},
		{"a last line with no newline", "one\r\ntwo",
			[][2]string{{"two", "TWO"}}, "one\r\nTWO"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(path, []byte(row.file), 0o600); err != nil {
				t.Fatal(err)
			}
			ledger := turn.NewReadLedger()
			ledger.Mark("f.txt", []byte(row.file))
			editor, err := NewEdit(dir)
			if err != nil {
				t.Fatal(err)
			}
			editor = editor.Reading(ledger)
			for _, edit := range row.edits {
				raw, _ := json.Marshal(map[string]string{"path": "f.txt", "old_string": edit[0], "new_string": edit[1]})
				result, err := editor.Run(context.Background(), raw)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(result.Content, "repaired") {
					t.Fatalf("only the line endings differed and the result blames the model:\n%s", result.Content)
				}
			}
			got, _ := os.ReadFile(path)
			if string(got) != row.want {
				t.Fatalf("the file is %q, want %q", got, row.want)
			}
		})
	}
}
