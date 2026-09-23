package library

import (
	"embed"
	"io/fs"
)

//go:embed models/*/*.yaml subscriptions/*.yaml
//go:embed general/rules/*.yaml dev/rules/*.yaml dev/go/rules/*.yaml
//go:embed qa/general/rules/*.yaml tools/*/rules/*.yaml tools/*/*.yaml
//go:embed qa/agents/*.md qa/references/*.md qa/general/skills/*.md
//go:embed general/references/*.md
var shipped embed.FS

func Files() fs.FS { return shipped }
