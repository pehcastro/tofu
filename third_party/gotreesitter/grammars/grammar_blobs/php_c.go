package grammarblobs

import _ "embed"

//go:embed php_c.bin
var blobPhpC []byte

func PhpC() []byte { return blobPhpC }
