package grammarblobs

import _ "embed"

//go:embed julia_c.bin
var blobJuliaC []byte

func JuliaC() []byte { return blobJuliaC }
