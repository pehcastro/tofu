package grammarblobs

import _ "embed"

//go:embed json_c.bin
var blobJsonC []byte

func JsonC() []byte { return blobJsonC }
