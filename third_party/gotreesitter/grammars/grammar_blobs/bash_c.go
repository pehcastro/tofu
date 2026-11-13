package grammarblobs

import _ "embed"

//go:embed bash_c.bin
var blobBashC []byte

func BashC() []byte { return blobBashC }
