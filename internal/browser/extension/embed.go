package extension

import "embed"

//go:embed manifest.json background.js popup.html popup.js snapshot.js
var Files embed.FS
