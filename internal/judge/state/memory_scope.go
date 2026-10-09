package state

import (
	"strings"

	"tofu/internal/judge/ledger"
)

const (
	MemoryScopePoint           = "memory_scope"
	MemoryScopeRef             = "memory_scope@1"
	MemoryScopeQuestion        = "scope"
	MemoryNamesPrivateQuestion = "names_private"
	MemoryScopeNone            = "none"
)

type MemoryScopeState struct {
	Candidate             string `json:"candidate"`
	Said                  string `json:"said"`
	Repository            string `json:"repository"`
	RepositoryTofuIgnored bool   `json:"repository_tofu_ignored"`
}

func BuildMemoryScope(in MemoryScopeState) ([]byte, string, error) {
	canon, err := ledger.Canonical(in)
	return canon, deriveVersion(MemoryScopePoint, MemoryScopeState{}), err
}

func MemoryScopeOf(option string) string { return strings.ReplaceAll(option, "_", "-") }

func RefusesProjectLocal(namesPrivate float64, answered bool) bool {
	return !answered || namesPrivate >= 1-namesPrivate
}
