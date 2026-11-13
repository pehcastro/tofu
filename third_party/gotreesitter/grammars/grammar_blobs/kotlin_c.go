package grammarblobs

import _ "embed"

//go:embed kotlin_c.bin
var blobKotlinC []byte

func KotlinC() []byte { return blobKotlinC }
