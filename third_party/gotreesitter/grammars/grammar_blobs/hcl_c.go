package grammarblobs

import _ "embed"

//go:embed hcl_c.bin
var blobHclC []byte

func HclC() []byte { return blobHclC }
