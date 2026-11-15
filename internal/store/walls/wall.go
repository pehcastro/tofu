package walls

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
)

type Op string

const (
	OpVerb           Op = "verb"
	OpTool           Op = "tool"
	OpTurn           Op = "turn"
	OpTurnStep       Op = "turn_step"
	OpRequest        Op = "request"
	OpFirstToken     Op = "first_token"
	OpLastByte       Op = "last_byte"
	OpHTTPHeaders    Op = "http_headers"
	OpHTTPStream     Op = "http_stream"
	OpPermissionWait Op = "permission_wait"
	OpHook           Op = "hook"
	OpJev            Op = "jev"
	OpAdvisor        Op = "advisor"
	OpForkBlocked    Op = "fork_blocked"
	OpBrowserStep    Op = "browser_step"
	OpShell          Op = "shell"
	OpSift           Op = "sift"
	OpStartup        Op = "startup"
	OpStartupStep    Op = "startup_step"
	OpSpawn          Op = "spawn"
	OpSpawnStep      Op = "spawn_step"
	OpFirstFrame     Op = "first_frame"
	OpFrame          Op = "frame"
	OpKey            Op = "key"
	OpAbsorb         Op = "absorb"
	OpSend           Op = "send"
	OpSessionOpen    Op = "session_open"
	OpSessionResume  Op = "session_resume"
	OpCompaction     Op = "compaction"
	OpMemoryRequest  Op = "memory_request"
	OpUndoSnapshot   Op = "undo_snapshot"
	OpHostStart      Op = "host_start"
	OpHostJoin       Op = "host_join"
	OpWire           Op = "wire"
	OpCron           Op = "cron"
	OpDoctorStep     Op = "doctor_step"
)

func Ops() []Op {
	return []Op{OpVerb, OpTool, OpTurn, OpTurnStep, OpRequest, OpFirstToken, OpLastByte, OpHTTPHeaders, OpHTTPStream, OpPermissionWait, OpHook, OpJev,
		OpAdvisor, OpForkBlocked, OpBrowserStep, OpShell, OpSift, OpStartup, OpStartupStep, OpSpawn, OpSpawnStep, OpFirstFrame,
		OpFrame, OpKey, OpAbsorb, OpSend, OpSessionOpen, OpSessionResume, OpCompaction, OpMemoryRequest, OpUndoSnapshot, OpHostStart, OpHostJoin, OpWire, OpCron, OpDoctorStep}
}

func ParseOp(text string) (Op, error) {
	if op := Op(text); slices.Contains(Ops(), op) {
		return op, nil
	}
	return "", fmt.Errorf("%q is not an op; one of %s", text, fmt.Sprint(Ops()))
}

type Wall struct {
	Op        Op     `json:"op"`
	Name      string `json:"name"`
	MS        int64  `json:"ms"`
	At        int64  `json:"at"`
	Session   string `json:"session,omitempty"`
	OK        bool   `json:"ok"`
	OutsideMS *int64 `json:"outside_ms,omitempty"`
	N         int    `json:"n,omitempty"`
	MaxMS     int64  `json:"max_ms,omitempty"`
}

const (
	dirName   = "walls"
	daySuffix = ".jsonl"
	dayLayout = time.DateOnly
)

type Writer struct {
	State string
	mu    sync.Mutex
	day   string
	file  *os.File
}

func Append(state string, wall Wall) error {
	once := Writer{State: state}
	return errors.Join(once.Append(wall), once.Close())
}

func (w *Writer) Append(wall Wall) error {
	line, err := json.Marshal(wall)
	if err != nil {
		return err
	}
	if len(line) >= konst.WallLineBytes {
		return fmt.Errorf("walls: a %d byte line is over the %d bytes one append keeps whole", len(line), konst.WallLineBytes)
	}
	day := time.UnixMilli(wall.At).UTC()
	w.mu.Lock()
	defer w.mu.Unlock()
	if name := day.Format(dayLayout); name != w.day {
		if err := w.closeDay(); err != nil {
			return err
		}
		if w.file, err = openDay(w.State, day); w.file == nil {
			return err
		}
		w.day = name
	}
	_, err = w.file.Write(append(line, '\n'))
	return err
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeDay()
}

func (w *Writer) closeDay() error {
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file, w.day = nil, ""
	return err
}

func openDay(state string, day time.Time) (*os.File, error) {
	if state == "" {
		return nil, errors.New("walls: no project state folder to write into")
	}
	dir := filepath.Join(state, dirName)
	path := filepath.Join(dir, day.Format(dayLayout)+daySuffix)
	_, statErr := os.Stat(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil || !errors.Is(statErr, os.ErrNotExist) {
		return file, err
	}
	oldest := day.Truncate(24*time.Hour).AddDate(0, 0, -konst.WallDaysKept)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		kept, isDay := strings.CutSuffix(entry.Name(), daySuffix)
		if keptDay, err := time.Parse(dayLayout, kept); isDay && err == nil && keptDay.Before(oldest) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return file, nil
}
