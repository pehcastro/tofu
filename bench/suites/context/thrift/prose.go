package thrift

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/konst"
)

type Redundancy struct {
	Tool               string
	Artifacts          int
	Bytes              int64
	BlankBytes         int64
	DuplicateBytes     int64
	TrailingBytes      int64
	GzipBytes          int64
	ArmKeptBytes       int64
	UniqueLines        int
	ArmKeptUniqueLines int
	OverCap            int
	UnderCapIfStripped int
}

func (r Redundancy) RemovableBytes() int64 {
	return r.BlankBytes + r.DuplicateBytes + r.TrailingBytes
}

type ArtifactSkip struct {
	Handle string
	Reason string
}

type Prose struct {
	Dir      string
	Handles  int
	Rows     []Redundancy
	Whole    Redundancy
	Skips    []ArtifactSkip
	ArmBytes int
	ArmHead  int
	ArmTail  int
}

func MeasureProse(artifactsDir string, sessions []corpus.Turn) Prose {
	toolOf := map[string]string{}
	order := []string{}
	for _, session := range sessions {
		for _, call := range sessionCallsOf(session) {
			if call.ResultHandle == "" {
				continue
			}
			if _, seen := toolOf[call.ResultHandle]; seen {
				continue
			}
			toolOf[call.ResultHandle] = call.Tool
			order = append(order, call.ResultHandle)
		}
	}
	sort.Strings(order)
	head := konst.TurnResultBytesCap / 2
	measured := Prose{
		Dir:      artifactsDir,
		Handles:  len(order),
		ArmBytes: konst.TurnResultBytesCap,
		ArmHead:  head,
		ArmTail:  konst.TurnResultBytesCap - head,
	}
	byTool := map[string]*Redundancy{}
	tools := []string{}
	for _, handle := range order {
		content, err := os.ReadFile(filepath.Join(artifactsDir, handle+".bin"))
		if err != nil {
			measured.Skips = append(measured.Skips, ArtifactSkip{Handle: handle, Reason: err.Error()})
			continue
		}
		tool := toolOf[handle]
		row, ok := byTool[tool]
		if !ok {
			row = &Redundancy{Tool: tool}
			byTool[tool] = row
			tools = append(tools, tool)
		}
		one := measureOne(string(content), konst.TurnResultBytesCap)
		row.Artifacts++
		addInto(row, one)
		measured.Whole.Artifacts++
		addInto(&measured.Whole, one)
	}
	sort.Strings(tools)
	for _, tool := range tools {
		measured.Rows = append(measured.Rows, *byTool[tool])
	}
	measured.Whole.Tool = "all"
	return measured
}

func addInto(row *Redundancy, one Redundancy) {
	row.Bytes += one.Bytes
	row.BlankBytes += one.BlankBytes
	row.DuplicateBytes += one.DuplicateBytes
	row.TrailingBytes += one.TrailingBytes
	row.GzipBytes += one.GzipBytes
	row.ArmKeptBytes += one.ArmKeptBytes
	row.UniqueLines += one.UniqueLines
	row.ArmKeptUniqueLines += one.ArmKeptUniqueLines
	row.OverCap += one.OverCap
	row.UnderCapIfStripped += one.UnderCapIfStripped
}

func measureOne(content string, armBytes int) Redundancy {
	one := Redundancy{Bytes: int64(len(content))}
	seen := map[string]bool{}
	for _, raw := range strings.SplitAfter(content, "\n") {
		if raw == "" {
			continue
		}
		width := int64(len(raw))
		line := strings.TrimSuffix(raw, "\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			one.BlankBytes += width
			continue
		}
		if seen[trimmed] {
			one.DuplicateBytes += width
			continue
		}
		seen[trimmed] = true
		one.TrailingBytes += int64(len(line) - len(strings.TrimRight(line, " \t\r")))
	}
	one.UniqueLines = len(seen)
	one.GzipBytes = gzipSize(content)
	kept := content
	if one.Bytes > int64(armBytes) {
		one.OverCap = 1
		if one.Bytes-one.RemovableBytes() <= int64(armBytes) {
			one.UnderCapIfStripped = 1
		}
		head := armBytes / 2
		kept = content[:head] + content[len(content)-(armBytes-head):]
	}
	one.ArmKeptBytes = int64(len(kept))
	inArm := map[string]bool{}
	for _, line := range strings.Split(kept, "\n") {
		if trimmed := strings.TrimSpace(line); seen[trimmed] {
			inArm[trimmed] = true
		}
	}
	one.ArmKeptUniqueLines = len(inArm)
	return one
}

func gzipSize(content string) int64 {
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(content)); err != nil {
		return 0
	}
	if err := writer.Close(); err != nil {
		return 0
	}
	return int64(out.Len())
}
