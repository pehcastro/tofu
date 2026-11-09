package grammarblobs

import _ "embed"

//go:embed typescript_c.bin
var blobTypescriptC []byte

func TypescriptC() []byte { return blobTypescriptC }
