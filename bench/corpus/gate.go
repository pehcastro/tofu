package corpus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"tofu/internal/dummy/toolgate"
	"tofu/internal/judge/gate"
)

const TruncationMark = " ...[truncated]"

type Label string

const (
	Proceed Label = "proceed"
	Block   Label = "block"
)

type Labeller string

const (
	Owner Labeller = "owner"
	Agent Labeller = "agent"
)

type Record struct {
	ID          string   `json:"id"`
	Origin      string   `json:"origin"`
	Recorded    bool     `json:"recorded"`
	RecordedAt  string   `json:"recorded_at"`
	StateShape  string   `json:"state_shape"`
	Label       Label    `json:"label"`
	LabelBy     Labeller `json:"label_by"`
	LabelNote   string   `json:"label_note"`
	HookRefusal string   `json:"hook_refusal"`
	State       any      `json:"state"`
}

type Split struct {
	CreatedAt     string   `json:"created_at"`
	Method        string   `json:"method"`
	HeldoutDigest string   `json:"heldout_digest"`
	Train         []string `json:"train"`
	Heldout       []string `json:"heldout"`
}

func (r Record) Command() string {
	state, ok := r.State.(map[string]any)
	if !ok {
		return ""
	}
	input, ok := state["input"].(map[string]any)
	if !ok {
		return ""
	}
	command, _ := input["command"].(string)
	return command
}

func CutCommands(records []Record) map[string]bool {
	ids := map[string]bool{}
	for _, record := range records {
		if strings.Contains(record.Command(), TruncationMark) {
			ids[record.ID] = true
		}
	}
	return ids
}

func GateRecords() ([]Record, error) {
	return gateRecordsIn(toolgate.Cases)
}

func GateWholeCommandRecords() ([]Record, error) {
	return gateRecordsIn(toolgate.CasesWhole)
}

func HomeGateRecords() ([]Record, error) {
	files, err := gate.HomeCasesFiles()
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		read, err := parseGateRecords(file, raw)
		if err != nil {
			return nil, err
		}
		records = append(records, read...)
	}
	return records, nil
}

func gateRecordsIn(path string) ([]Record, error) {
	raw, err := toolgate.Files.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseGateRecords(path, raw)
}

func parseGateRecords(path string, raw []byte) ([]Record, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var records []Record
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Bytes()
		if len(bytes.TrimSpace(text)) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(text, &record); err != nil {
			return nil, fmt.Errorf("bench/corpus: %s line %d: %w", path, line, err)
		}
		if record.Label != Proceed && record.Label != Block {
			return nil, fmt.Errorf("bench/corpus: %s line %d: label %q is neither proceed nor block", path, line, record.Label)
		}
		if record.LabelBy != Owner && record.LabelBy != Agent {
			return nil, fmt.Errorf("bench/corpus: %s line %d: label_by %q is neither owner nor agent", path, line, record.LabelBy)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func GateSplit() (Split, error) {
	raw, err := toolgate.Files.ReadFile(toolgate.Split)
	if err != nil {
		return Split{}, err
	}
	var split Split
	if err := json.Unmarshal(raw, &split); err != nil {
		return Split{}, fmt.Errorf("bench/corpus: %s: %w", toolgate.Split, err)
	}
	return split, nil
}
