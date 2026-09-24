package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEachAppendedPromotionIsOneLineAndAnUnsetLogWritesNone(t *testing.T) {
	dir := t.TempDir()
	log := NewPromotionLog(dir)
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	rows := []Promotion{
		{At: at, Session: "turn-a", Action: ReachedIntoWork, EventID: "e1", EventKind: "tool", FreeArm: PlaceWork, Chose: PlaceChat},
		{At: at.Add(time.Minute), Session: "turn-a", Action: MovedKind, EventKind: "tool", FreeArm: PlaceChat, Chose: PlaceWork},
	}
	for _, row := range rows {
		if err := log.Append(row); err != nil {
			t.Fatalf("append %s: %v", row.Action, err)
		}
	}
	if err := PromotionLog("").Append(rows[0]); err != nil {
		t.Fatalf("appending to an unset log: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, PromotionsFileName))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for _, want := range rows {
		var got Promotion
		if err := decoder.Decode(&got); err != nil {
			t.Fatalf("decode the row for %s: %v", want.Action, err)
		}
		if got != want {
			t.Errorf("the log holds %+v, want %+v", got, want)
		}
	}
	if decoder.More() {
		t.Errorf("two appends and one unset log wrote more than two rows: %s", raw)
	}
}
