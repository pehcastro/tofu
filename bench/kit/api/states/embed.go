package states

import "embed"

//go:embed *.json
var files embed.FS

func Files() embed.FS { return files }
