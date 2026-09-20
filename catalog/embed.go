package catalog

import (
	"embed"
	"io/fs"
)

//go:embed models/*/*.yaml subscriptions/*.yaml
var shipped embed.FS

func Files() fs.FS { return shipped }
