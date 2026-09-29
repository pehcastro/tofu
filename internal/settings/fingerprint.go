package settings

import (
	"encoding/json"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
)

const (
	SubFingerprint        = "subFingerprint"
	FingerprintClaudeCode = "claudeCode"
	FingerprintCodex      = "codex"
)

type Fingerprint struct {
	ClaudeCode string `json:"claudeCode"`
	Codex      string `json:"codex"`
}

func PinnedFingerprint() Fingerprint {
	return Fingerprint{ClaudeCode: anthropic.PinnedClaudeCodeVersion, Codex: codex.PinnedCodexClientVersion}
}

func (s *Store) Fingerprint() Fingerprint {
	var declared Fingerprint
	_ = json.Unmarshal(s.objects[Global][SubFingerprint], &declared)
	pinned := PinnedFingerprint()
	return Fingerprint{
		ClaudeCode: anthropic.NewerVersion(pinned.ClaudeCode, declared.ClaudeCode),
		Codex:      anthropic.NewerVersion(pinned.Codex, declared.Codex),
	}
}

func (s *Store) saveFingerprint(scope Scope, key, version string) error {
	if err := s.reread(scope); err != nil {
		return err
	}
	held := map[string]any{}
	_ = json.Unmarshal(s.objects[scope][SubFingerprint], &held)
	held[key] = version
	encoded, err := json.Marshal(held)
	if err != nil {
		return err
	}
	s.objects[scope][SubFingerprint] = encoded
	return s.write(scope)
}
