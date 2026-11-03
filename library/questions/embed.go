package questions

import (
	"embed"
	"io/fs"
)

const (
	ToolGatePin = "tool_gate@6"
	AskPin      = "ask@1"
)

//go:embed *.yaml config/*.yaml tool_gate@6.yaml ask@1.yaml
var shipped embed.FS

func Files() fs.FS {
	return shipped
}
