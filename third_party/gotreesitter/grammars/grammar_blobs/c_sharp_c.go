package grammarblobs

import _ "embed"

//go:embed c_sharp_c.bin
var blobC_sharpC []byte

func C_sharpC() []byte { return blobC_sharpC }
