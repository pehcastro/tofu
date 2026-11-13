package grammarblobs

import _ "embed"

//go:embed solidity_c.bin
var blobSolidityC []byte

func SolidityC() []byte { return blobSolidityC }
