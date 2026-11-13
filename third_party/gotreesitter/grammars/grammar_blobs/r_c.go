package grammarblobs

import _ "embed"

//go:embed r_c.bin
var blobRC []byte

func RC() []byte { return blobRC }
