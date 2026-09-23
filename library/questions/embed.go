package questions

import (
	"embed"
	"io/fs"
)

//go:embed *.yaml config/*.yaml
var shipped embed.FS

func Files() fs.FS {
	return shipped
}
