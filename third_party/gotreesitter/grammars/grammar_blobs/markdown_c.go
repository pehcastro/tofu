package grammarblobs

import _ "embed"

//go:embed markdown_c.bin
var blobMarkdownC []byte

func MarkdownC() []byte { return blobMarkdownC }
