package main

import (
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/context/window"
	"tofu/internal/tools/contract"
)

func newContextTool(dir string) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	if err != nil {
		return nil, err
	}
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return nil, err
	}
	return window.Window{Sessions: sessions, Root: root}, nil
}
