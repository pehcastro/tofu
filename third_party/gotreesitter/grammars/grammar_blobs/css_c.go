package grammarblobs

import _ "embed"

//go:embed css_c.bin
var blobCssC []byte

func CssC() []byte { return blobCssC }
