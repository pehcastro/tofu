package questions

import (
	"embed"
	"io/fs"
)

//go:embed *.yaml
var shipped embed.FS

func Files() fs.FS {
	return shipped
}
