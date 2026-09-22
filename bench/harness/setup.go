package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"tofu/internal/rule"
	"tofu/internal/sys"
)

const (
	stockSetupName = "stock"
	stockRuleDir   = "library"
)

type Setup struct {
	Name         string   `json:"name"`
	RuleFiles    []string `json:"rule_files"`
	PromptBytes  int      `json:"prompt_bytes"`
	PromptSHA256 string   `json:"prompt_sha256"`
}

func SetupOf(name, ruleDir, prompt string) (Setup, error) {
	if name == "" {
		return Setup{}, fmt.Errorf("a setup with no name cannot be read back off a row, and %d prompt bytes were offered under it", len(prompt))
	}
	sum := sha256.Sum256([]byte(prompt))
	setup := Setup{Name: name, PromptBytes: len(prompt), PromptSHA256: hex.EncodeToString(sum[:])}
	if ruleDir == "" {
		return setup, nil
	}
	present, err := sys.IsDir(ruleDir)
	if err != nil {
		return Setup{}, err
	}
	if !present {
		return Setup{}, fmt.Errorf("setup %q names rule directory %s, which is not a directory, so the name would stand for nothing", name, ruleDir)
	}
	loaded, err := rule.LoadDir(ruleDir)
	if err != nil {
		return Setup{}, err
	}
	for _, r := range loaded {
		setup.RuleFiles = append(setup.RuleFiles, r.File)
	}
	sort.Strings(setup.RuleFiles)
	return setup, nil
}

func (s Setup) Line() string {
	if s.Name == "" {
		return "no setup was recorded beside this row, so nothing about it can be reproduced"
	}
	return fmt.Sprintf("%d rule files, prompt %d bytes, sha256 %s", len(s.RuleFiles), s.PromptBytes, s.PromptSHA256)
}
