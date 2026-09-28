package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/sys"
)

const logSuffix = ".jsonl"

const outcomeSuffix = ".outcome.jsonl"

const idRandomBytes = 16

const stateDirName = "states"

const stateSuffix = ".json"

const stateInlineCeiling = 4096

const stateExcerptBytes = 512

type Writer struct {
	dir    string
	now    func() time.Time
	redact sys.KeyRedactor
}

func NewWriter(dir string) *Writer {
	return NewWriterWithClock(dir, time.Now)
}

func NewWriterWithClock(dir string, now func() time.Time) *Writer {
	return &Writer{dir: dir, now: now, redact: sys.LoadKeyRedactor()}
}

func (w *Writer) Append(row Row) (Row, error) {
	if row.Reason != nil && row.Reason.Mode != ModeShadow && row.Reason.Mode != ModeEnforced {
		return Row{}, fmt.Errorf("ledger: a written row's mode must be shadow or enforced, got %q", string(row.Reason.Mode))
	}
	if row.At.IsZero() {
		row.At = w.now()
	}
	row.At = row.At.UTC().Truncate(time.Millisecond)
	row.Schema = SchemaVersion
	row.Outcome = nil
	if row.ID == "" {
		id, err := newID(row.At)
		if err != nil {
			return Row{}, err
		}
		row.ID = id
	}
	if row.State != nil {
		row.State = json.RawMessage(w.redact.Redact(string(row.State)))
	}
	if err := w.elideState(&row); err != nil {
		return Row{}, err
	}
	line, err := Canonical(row)
	if err != nil {
		return Row{}, err
	}
	if err := appendLine(filepath.Join(w.dir, row.Day()+logSuffix), line); err != nil {
		return Row{}, err
	}
	return row, nil
}

func (w *Writer) elideState(row *Row) error {
	body := row.State
	if len(body) <= stateInlineCeiling {
		return nil
	}
	name := row.ID + stateSuffix
	if err := sys.WriteFile(filepath.Join(w.dir, stateDirName, name), body, 0o644); err != nil {
		return err
	}
	row.State = nil
	row.StateElision = &StateElision{
		Bytes: len(body),
		Head:  strings.ToValidUTF8(string(body[:stateExcerptBytes]), ""),
		Tail:  strings.ToValidUTF8(string(body[len(body)-stateExcerptBytes:]), ""),
		File:  filepath.Join(stateDirName, name),
	}
	return nil
}

type outcomeRecord struct {
	ID      string  `json:"id"`
	Schema  int     `json:"schema"`
	Outcome Outcome `json:"outcome"`
}

func (w *Writer) Backfill(id string, outcome Outcome) error {
	day, err := dayOfID(id)
	if err != nil {
		return err
	}
	if outcome.Kind == "" {
		return errors.New("ledger: an outcome needs a kind")
	}
	if outcome.At.IsZero() {
		outcome.At = w.now()
	}
	outcome.At = outcome.At.UTC().Truncate(time.Millisecond)
	line, err := Canonical(outcomeRecord{ID: id, Schema: SchemaVersion, Outcome: outcome})
	if err != nil {
		return err
	}
	return appendLine(filepath.Join(w.dir, day+outcomeSuffix), line)
}

func newID(at time.Time) (string, error) {
	noise := make([]byte, idRandomBytes)
	if _, err := rand.Read(noise); err != nil {
		return "", err
	}
	return at.UTC().Format(dayLayout) + "-" + hex.EncodeToString(noise), nil
}

func dayOfID(id string) (string, error) {
	if len(id) < len(dayLayout) {
		return "", fmt.Errorf("ledger: %q is not a row id", id)
	}
	day := id[:len(dayLayout)]
	if _, err := time.Parse(dayLayout, day); err != nil {
		return "", fmt.Errorf("ledger: %q is not a row id: %w", id, err)
	}
	return day, nil
}

func appendLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
