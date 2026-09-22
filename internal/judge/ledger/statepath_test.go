package ledger

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/sys"
)

func TestARowNamingAFileOutsideTheLedgerIsRefused(t *testing.T) {
	outside := t.TempDir()
	dir := filepath.Join(outside, "log")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the ledger directory: %v", err)
	}
	bait := filepath.Join(outside, "pretend-secret.txt")
	if err := os.WriteFile(bait, []byte("PRETEND-KEY-WRITTEN-BY-THIS-TEST"), 0o600); err != nil {
		t.Fatalf("write the bait file: %v", err)
	}

	for _, named := range []string{"../pretend-secret.txt", `..\pretend-secret.txt`, "states/../../pretend-secret.txt", bait} {
		body, err := NewReader(dir).State(Row{StateElision: &StateElision{File: named}})
		var escaping EscapingStateError
		if !errors.As(err, &escaping) {
			t.Fatalf("State on a row naming %s returned %d bytes and %v, want a typed refusal", named, len(body), err)
		}
		if len(body) != 0 {
			t.Fatalf("the refusal of %s still returned %d bytes", named, len(body))
		}
		if escaping.File != named || !strings.Contains(escaping.Error(), "pretend-secret.txt") {
			t.Fatalf("the refusal does not name the path it refused: %+v, %q", escaping, escaping.Error())
		}
	}
}

func TestAMissingStateFileIsNotTheEscapeRefusal(t *testing.T) {
	dir := t.TempDir()
	_, err := NewReader(dir).State(Row{StateElision: &StateElision{File: filepath.Join(stateDirName, "never-written.json")}})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a state file that was never written reads as %v, want a missing file", err)
	}
	var escaping EscapingStateError
	if errors.As(err, &escaping) {
		t.Fatalf("a missing state row reads as an escape, and a missing state row is normal: %v", err)
	}
}

func TestAnElidedRowWrittenByTofuStillReads(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"command":"` + strings.Repeat("y", 8*1024) + `","tool":"bash"}`)
	stored, err := NewWriter(dir).Append(Row{Point: "tool_gate", Questions: "tool_gate", Version: 1, State: body})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	read, err := NewReader(dir).State(stored)
	if err != nil {
		t.Fatalf("the state tofu itself elided to %s does not read back: %v", stored.StateElision.File, err)
	}
	if string(read) != string(body) {
		t.Fatalf("the state read back is %d bytes, want the whole %d", len(read), len(body))
	}
}

func TestARecordedElidedRowOnDiskStillReads(t *testing.T) {
	dir := filepath.Join(sys.SourceRoot(), ".tofu", "log")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("skip: this checkout has no .tofu/log, so there is no recorded row to read")
	}
	reader := NewReader(dir)
	var elided Row
	if _, err := reader.Each(Filter{}, func(row Row) error {
		if row.StateElision == nil {
			return nil
		}
		elided = row
		return errRowFound
	}); err != nil && !errors.Is(err, errRowFound) {
		t.Fatalf("reading the recorded ledger: %v", err)
	}
	if elided.StateElision == nil {
		t.Skip("skip: no row in the recorded ledger carries an elided state")
	}
	state, err := reader.State(elided)
	if err != nil {
		t.Fatalf("a recorded row is refused by the new guard: %v", err)
	}
	if len(state) != elided.StateElision.Bytes || !json.Valid(state) {
		t.Fatalf("the recorded state read back as %d bytes, the row says %d", len(state), elided.StateElision.Bytes)
	}
}
