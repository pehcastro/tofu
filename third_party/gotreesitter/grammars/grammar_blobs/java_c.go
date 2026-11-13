package grammarblobs

import _ "embed"

//go:embed java_c.bin
var blobJavaC []byte

func JavaC() []byte { return blobJavaC }
