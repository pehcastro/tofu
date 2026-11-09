package grammarblobs

import _ "embed"

//go:embed go_c.bin
var blobGoC []byte

func GoC() []byte { return blobGoC }
