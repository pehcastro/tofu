package grammarblobs

import _ "embed"

//go:embed cmake_c.bin
var blobCmakeC []byte

func CmakeC() []byte { return blobCmakeC }
