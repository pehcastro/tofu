package extension

import "embed"

//go:embed manifest.json background.js snapshot.js icon-16.png icon-32.png icon-48.png icon-128.png
var Files embed.FS
