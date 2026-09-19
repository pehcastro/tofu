package turn

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"boji/internal/sys"
)

const logSuffix = ".jsonl"

type Writer struct {
	dir string
	now func() time.Time
}

func NewWriter(dir string) *Writer {
	return &Writer{dir: dir, now: time.Now}
}

func (w *Writer) Append(row Row) (Row, error) {
	if row.ID == "" {
		return Row{}, errors.New("turn: a written row needs an id")
	}
	if row.At.IsZero() {
		row.At = w.now()
	}
	row.At = row.At.UTC().Truncate(time.Millisecond)
	row.Schema = SchemaVersion
	line, err := json.Marshal(row)
	if err != nil {
		return Row{}, err
	}
	if err := appendLine(filepath.Join(w.dir, row.Day()+logSuffix), line); err != nil {
		return Row{}, err
	}
	return row, nil
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

func Dir() (string, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "sessions"), nil
}
