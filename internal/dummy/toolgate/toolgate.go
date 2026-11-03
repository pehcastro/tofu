package toolgate

import "embed"

const (
	Dir        = "internal/dummy/toolgate"
	Cases      = "cases.jsonl"
	CasesWhole = "cases-whole.jsonl"
	Split      = "split.json"
	Heldout    = "heldout-2026-09-19.jsonl"
)

//go:embed cases.jsonl cases-whole.jsonl split.json heldout-2026-09-19.jsonl
var Files embed.FS
