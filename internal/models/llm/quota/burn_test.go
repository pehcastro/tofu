package quota

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBurnFollowsEachAccountWindowSinceItsLastReset(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	resets := now.Add(4 * time.Hour)
	dir := t.TempDir()
	reading := func(account int64, at time.Time, window string, used float64, resetsAt time.Time) {
		t.Helper()
		if err := AppendReading(dir, Reading{Provider: "claude", Account: account, At: at, Windows: []ReadingWindow{{ID: window, Used: used, ResetsAt: resetsAt}}}); err != nil {
			t.Fatal(err)
		}
	}
	reading(1, now.AddDate(0, 0, -3), "5h", 0.9, now.AddDate(0, 0, -3))
	reading(1, now.Add(-3*time.Hour), "5h", 0.80, now.Add(-time.Hour))
	reading(1, now.Add(-time.Hour), "5h", 0.10, resets)
	reading(1, now, "5h", 0.30, resets)
	reading(1, now.Add(-30*time.Minute), "5h", 0.20, resets)
	reading(2, now.Add(-2*time.Hour), "5h", 0.50, resets)
	reading(2, now, "5h", 0.60, resets)
	reading(2, now, "7d", 0.40, now.Add(time.Hour))
	reading(1, now.Add(-time.Hour), "7d", 0.20, now.AddDate(0, 0, 3))
	reading(1, now.Add(-time.Minute), "7d", 0.20, now.AddDate(0, 0, 3))
	file, err := os.OpenFile(filepath.Join(dir, now.Format(time.DateOnly)+".jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"provider":"claude","acc`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	readings, torn, err := ReadReadings(dir, now.AddDate(0, 0, -1))
	if err != nil || torn != 1 {
		t.Fatalf("read: %v, %d torn lines, want the one", err, torn)
	}
	burns := Burns(readings)
	fullAt := func(at time.Time) *time.Time { return &at }
	rate := func(perHour float64) *float64 { return &perHour }
	want := []Burn{
		{Provider: "claude", Account: 1, Window: "5h", Used: 0.30, ResetsAt: resets, Samples: 3, PerHour: rate(0.2), FullAt: fullAt(now.Add(210 * time.Minute))},
		{Provider: "claude", Account: 1, Window: "7d", Used: 0.20, ResetsAt: now.AddDate(0, 0, 3), Samples: 2, PerHour: rate(0)},
		{Provider: "claude", Account: 2, Window: "5h", Used: 0.60, ResetsAt: resets, Samples: 2, PerHour: rate(0.05)},
		{Provider: "claude", Account: 2, Window: "7d", Used: 0.40, ResetsAt: now.Add(time.Hour), Samples: 1},
	}
	if len(burns) != len(want) {
		t.Fatalf("%d burns, want %d: %+v", len(burns), len(want), burns)
	}
	for i := range want {
		got := burns[i]
		if got.PerHour != nil {
			rounded := float64(int(*got.PerHour*1e6+0.5)) / 1e6
			got.PerHour = &rounded
		}
		if got.FullAt != nil {
			rounded := got.FullAt.Round(time.Second)
			got.FullAt = &rounded
		}
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("burn %d\n got %+v %v %v\nwant %+v %v %v", i, got, deref(got.PerHour), got.FullAt, want[i], deref(want[i].PerHour), want[i].FullAt)
		}
	}
}

func deref(rate *float64) any {
	if rate == nil {
		return nil
	}
	return *rate
}
