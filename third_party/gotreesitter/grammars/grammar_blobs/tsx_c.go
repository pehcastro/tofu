package grammarblobs

import _ "embed"

//go:embed tsx_c.bin
var blobTsxC []byte

func TsxC() []byte { return blobTsxC }
