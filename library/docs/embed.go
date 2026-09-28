package docs

import (
	"embed"
	"io/fs"
)

//go:embed *.md index.yaml
var pages embed.FS

func Files() fs.FS { return pages }
