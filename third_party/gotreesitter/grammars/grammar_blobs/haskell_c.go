package grammarblobs

import _ "embed"

//go:embed haskell_c.bin
var blobHaskellC []byte

func HaskellC() []byte { return blobHaskellC }
