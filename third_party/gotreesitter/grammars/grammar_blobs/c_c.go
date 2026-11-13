package grammarblobs

import _ "embed"

//go:embed c_c.bin
var blobCC []byte

func CC() []byte { return blobCC }
