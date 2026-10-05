package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"tofu/internal/konst"
	"tofu/internal/sys"
)

const promptHistoryName = "prompts.jsonl"

type PromptHistory struct {
	path   string
	redact sys.KeyRedactor
	newest string
}

type promptLine struct {
	Prompt string `json:"prompt"`
}

func OpenPromptHistory(dir string) *PromptHistory {
	return &PromptHistory{path: filepath.Join(dir, promptHistoryName), redact: sys.LoadKeyRedactor()}
}

func encodedPrompt(prompt string) ([]byte, error) {
	line, err := json.Marshal(promptLine{prompt})
	return append([]byte{'\n'}, line...), err
}

func (h *PromptHistory) Add(prompt string) error {
	prompt = h.redact.Redact(prompt)
	if prompt == "" || prompt == h.newest || len(prompt) > konst.PromptHistoryEntryBytes {
		return nil
	}
	line, err := encodedPrompt(prompt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = file.Write(line)
	if err := errors.Join(err, file.Close()); err != nil {
		return err
	}
	h.newest = prompt
	if info, err := os.Stat(h.path); err == nil && info.Size() > konst.PromptHistoryFileBytes {
		_ = h.trim()
	}
	return nil
}

func (h *PromptHistory) Prompts() []string {
	prompts := slices.Compact(h.read())
	slices.Reverse(prompts)
	return prompts[:min(len(prompts), konst.PromptHistoryEntries)]
}

func (h *PromptHistory) read() []string {
	raw, _ := os.ReadFile(h.path)
	var prompts []string
	for line := range bytes.SplitSeq(raw, []byte{'\n'}) {
		var parsed promptLine
		if json.Unmarshal(line, &parsed) == nil && parsed.Prompt != "" {
			prompts = append(prompts, parsed.Prompt)
		}
	}
	return prompts
}

func (h *PromptHistory) trim() error {
	prompts := h.read()
	var lines [][]byte
	size := 0
	for at := len(prompts) - 1; at >= 0 && len(lines) < konst.PromptHistoryEntries; at-- {
		line, err := encodedPrompt(prompts[at])
		if err != nil || size+len(line) > konst.PromptHistoryKeptBytes {
			break
		}
		lines, size = append(lines, line), size+len(line)
	}
	slices.Reverse(lines)
	return sys.WriteFile(h.path, bytes.Join(lines, nil), 0o600)
}
