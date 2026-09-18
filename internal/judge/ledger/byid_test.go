package ledger

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestByIDFindsARowAmongTenThousandWithoutLoadingTheDayIntoMemory(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	const rows = 10000
	var targetID string
	for i := 0; i < rows; i++ {
		row := sampleRow("tool_gate", VerdictAsk, start.Add(time.Duration(i)*time.Second))
		row.RequestID = fmt.Sprintf("or-req-%05d", i)
		stored, err := writer.Append(row)
		if err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if i == rows-1 {
			targetID = stored.ID
		}
	}

	var bytesOnDisk int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		bytesOnDisk += info.Size()
	}

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	started := time.Now()
	row, ok, err := NewReader(dir).ByID(targetID)
	took := time.Since(started)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if !ok {
		t.Fatal("the last row of 10,000 was not found")
	}
	if row.ID != targetID {
		t.Fatalf("ByID returned %q, want %q", row.ID, targetID)
	}
	if row.RequestID != fmt.Sprintf("or-req-%05d", rows-1) {
		t.Fatalf("ByID returned the wrong row: %+v", row)
	}

	runtime.GC()
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)

	t.Logf("%d rows, %d bytes on disk, ByID found the last row in %s, heap changed by %d B across the lookup",
		rows, bytesOnDisk, took.Round(time.Millisecond), growth)

	const growthCeiling = 4 << 20
	if growth > growthCeiling {
		t.Fatalf("ByID grew the heap by %d B over %d B of ledger, the ceiling is %d B", growth, bytesOnDisk, growthCeiling)
	}
}

func TestByIDRejectsAnIDThatIsNotShapedLikeOne(t *testing.T) {
	_, ok, err := NewReader(t.TempDir()).ByID("not-a-row-id")
	if err == nil {
		t.Fatal("an id with no day must fail, it got no error")
	}
	if ok {
		t.Fatal("an id with no day must not report a hit")
	}
}

func TestByIDMissesWhenTheDayHasNoSuchRow(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 18, 9, 0, 0, 0, time.UTC)
	if _, err := NewWriter(dir).Append(sampleRow("tool_gate", VerdictAsk, at)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	row, ok, err := NewReader(dir).ByID("2026-09-18-000000000000000000000000000000")
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}
	if ok {
		t.Fatalf("a row that was never written must not be found, got %+v", row)
	}
}
