package recall

import "strings"

type DropReason string

const (
	DroppedAged   DropReason = "aged"
	DroppedJudged DropReason = "judged"
)

type Drop struct {
	Step        int        `json:"step"`
	Tool        string     `json:"tool"`
	Handle      string     `json:"handle"`
	Path        string     `json:"path,omitempty"`
	Bytes       int        `json:"bytes"`
	TokensFreed int        `json:"tokens_freed"`
	Reason      DropReason `json:"reason"`
	Decision    string     `json:"decision,omitempty"`
}

func AlreadyDropped(text string) bool {
	return strings.Contains(text, " result that stood here is held whole in artifact ")
}
