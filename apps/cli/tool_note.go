package main

import (
	"tofu/internal/tools/agents/note"
	"tofu/internal/tools/contract"
)

func newNoteTool(string) (contract.Tool, error) {
	return note.Note{}, nil
}
