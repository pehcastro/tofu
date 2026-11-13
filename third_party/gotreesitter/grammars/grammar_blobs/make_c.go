package grammarblobs

import _ "embed"

//go:embed make_c.bin
var blobMakeC []byte

func MakeC() []byte { return blobMakeC }
