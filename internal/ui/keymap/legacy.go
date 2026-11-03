package keymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

const (
	legacyFence = "// tofu-ctrl-k:"
	legacyEntry = `  {
    "context": "Terminal",
    "bindings": {
      "ctrl-k": ["terminal::SendKeystroke", "ctrl-k"]
    }
  }`
)

func legacyBlock(comma bool) []byte {
	return fmt.Appendf(nil, "%s\n  %sbegin comma=%t\n%s\n  %send\n", leadingComma(comma), legacyFence, comma, legacyEntry, legacyFence)
}

func legacyState(path string) (State, error) {
	source, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Missing, nil
	}
	if err != nil {
		return "", err
	}
	array, err := parseJSONC(source)
	if err != nil {
		return "", err
	}
	marked := bytes.Contains(source, []byte(legacyFence+"begin")) || bytes.Contains(source, []byte(legacyFence+"end"))
	if marked && !bytes.Contains(source, legacyBlock(false)) && !bytes.Contains(source, legacyBlock(true)) {
		return "", errors.New("managed binding was modified; refusing to change it automatically")
	}
	forwarded := false
	for _, entry := range array.entries {
		action, bound := entry.Bindings["ctrl-k"]
		if !bound || !strings.Contains(entry.Context, terminalContext) {
			continue
		}
		var parts []string
		if json.Unmarshal(action, &parts) != nil || !slices.Equal(parts, []string{zedForward, "ctrl-k"}) {
			return Conflicting, nil
		}
		forwarded = true
	}
	switch {
	case forwarded && marked:
		return Managed, nil
	case forwarded:
		return AlreadyConfigured, nil
	case marked:
		return "", errors.New("managed binding marker exists without a forwarding binding")
	}
	return NotConfigured, nil
}

func legacyUndo(path string) (status, backup string, err error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	for _, comma := range []bool{true, false} {
		block := legacyBlock(comma)
		at := bytes.Index(source, block)
		if at < 0 {
			continue
		}
		result := slices.Concat(source[:at], source[at+len(block):])
		if _, err := parseJSONC(result); err != nil {
			return "", "", fmt.Errorf("generated keymap failed validation: %w", err)
		}
		backup, err := writeManaged(path, source, result, false)
		if err != nil {
			return "", backup, err
		}
		return "removed tofu Ctrl+K binding", backup, nil
	}
	return nothingManaged, "", nil
}
