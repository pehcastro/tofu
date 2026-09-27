package library

import (
	"embed"
	"io/fs"
)

//go:embed models/*/*.yaml subscriptions/*.yaml
//go:embed general/rules/*.yaml dev/rules/*.yaml dev/go/rules/*.yaml
//go:embed dev/agents/*.md dev/references/*.md dev/typescript/rules/*.yaml dev/typescript/references/*.md
//go:embed qa/general/rules/*.yaml tools/*/rules/*.yaml tools/*/*.yaml
//go:embed qa/agents/*.md qa/references/*.md qa/general/skills/*.md
//go:embed general/references/*.md general/agents/*.md decisions/*.yaml
//go:embed web/*.yaml web/search/*.yaml
var shipped embed.FS

func Files() fs.FS { return shipped }
