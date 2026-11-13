package grammarblobs

import _ "embed"

//go:embed powershell_c.bin
var blobPowershellC []byte

func PowershellC() []byte { return blobPowershellC }
