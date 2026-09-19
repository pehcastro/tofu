package stopcheck

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"boji/internal/judge/jev"
)

const repoRoot = "../.."

func TestLiveBatteryOverEveryRecordedStep(t *testing.T) {
	if os.Getenv("BOJI_LIVE") != "1" {
		t.Skip("set BOJI_LIVE=1 to run both arms against the real jev route")
	}
	key, err := jev.Key(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Fatalf("no credential: %v", err)
	}
	turns, skipped, err := ReadSessions(sessionsDir)
	if err != nil {
		t.Fatalf("ReadSessions: %v", err)
	}
	battery, err := New(repoRoot, key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result, err := battery.Run(context.Background(), turns, skipped)
	if err != nil {
		t.Fatalf("the battery stopped: %v", err)
	}
	cheap, typed := result.Agreements()
	t.Logf("%s", cheap.Line())
	t.Logf("%s", typed.Line())
	if typed.Total < 40 {
		t.Errorf("the battery decided %d labelled steps, BOJI-079 asks for at least forty", typed.Total)
	}

	body := Render(result)
	path := "report-" + result.GeneratedAt.UTC().Format("2006-01-02") + ".md"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	t.Logf("wrote bench/stopcheck/%s", path)
}
