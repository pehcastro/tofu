package grammarblobs

import _ "embed"

//go:embed javascript_c.bin
var blobJavascriptC []byte

func JavascriptC() []byte { return blobJavascriptC }
