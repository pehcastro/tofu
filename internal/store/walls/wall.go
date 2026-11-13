package walls

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/platform/fs"
)

type Op string

const (
	OpVerb           Op = "verb"
	OpTool           Op = "tool"
	OpTurn           Op = "turn"
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
	return []Op{OpVerb, OpTool, OpTurn, OpRequest, OpFirstToken, OpLastByte, OpHTTPHeaders, OpHTTPStream, OpPermissionWait, OpHook, OpJev,
		OpAdvisor, OpForkBlocked, OpBrowserStep, OpShell, OpSift, OpStartup, OpStartupStep, OpSpawn, OpSpawnStep, OpFirstFrame,
		OpSessionOpen, OpSessionResume, OpCompaction, OpMemoryRequest, OpUndoSnapshot, OpHostStart, OpHostJoin, OpWire, OpCron, OpDoctorStep}
}

func ParseOp(text string) (Op, error) {
	if op := Op(text); slices.Contains(Ops(), op) {
		return op, nil
	}
	return "", fmt.Errorf("%q is not an op; one of %s", text, fmt.Sprint(Ops()))
}

type Wall struct {
	Op      Op     `json:"op"`
	Name    string `json:"name"`
	MS      int64  `json:"ms"`
	At      int64  `json:"at"`
	Session string `json:"session,omitempty"`
	OK      bool   `json:"ok"`
}

const (
	dirName   = "walls"
	daySuffix = ".jsonl"
	dayLayout = time.DateOnly
)

func Append(state string, wall Wall) error {
	if isDir, _ := fs.IsDir(state); !isDir {
		return nil
	}
	line, err := json.Marshal(wall)
	if err != nil {
		return err
	}
	if len(line) >= konst.WallLineBytes {
		return fmt.Errorf("walls: a %d byte line is over the %d bytes one append keeps whole", len(line), konst.WallLineBytes)
	}
	day, dir := time.UnixMilli(wall.At).UTC(), filepath.Join(state, dirName)
	path := filepath.Join(dir, day.Format(dayLayout)+daySuffix)
	_, statErr := os.Stat(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(append(line, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return err
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	oldest := day.Truncate(24*time.Hour).AddDate(0, 0, -konst.WallDaysKept)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		kept, isDay := strings.CutSuffix(entry.Name(), daySuffix)
		if keptDay, err := time.Parse(dayLayout, kept); isDay && err == nil && keptDay.Before(oldest) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}
