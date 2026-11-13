package grammarblobs

import _ "embed"

//go:embed html_c.bin
var blobHtmlC []byte

func HtmlC() []byte { return blobHtmlC }
