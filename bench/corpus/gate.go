package corpus

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed gate/cases.jsonl gate/split.json
var gateFiles embed.FS

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

func GateRecords() ([]Record, error) {
	raw, err := gateFiles.ReadFile("gate/cases.jsonl")
	if err != nil {
		return nil, err
	}
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
			return nil, fmt.Errorf("bench/corpus: gate/cases.jsonl line %d: %w", line, err)
		}
		if record.Label != Proceed && record.Label != Block {
			return nil, fmt.Errorf("bench/corpus: gate/cases.jsonl line %d: label %q is neither proceed nor block", line, record.Label)
		}
		if record.LabelBy != Owner && record.LabelBy != Agent {
			return nil, fmt.Errorf("bench/corpus: gate/cases.jsonl line %d: label_by %q is neither owner nor agent", line, record.LabelBy)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func GateSplit() (Split, error) {
	raw, err := gateFiles.ReadFile("gate/split.json")
	if err != nil {
		return Split{}, err
	}
	var split Split
	if err := json.Unmarshal(raw, &split); err != nil {
		return Split{}, fmt.Errorf("bench/corpus: gate/split.json: %w", err)
	}
	return split, nil
}
