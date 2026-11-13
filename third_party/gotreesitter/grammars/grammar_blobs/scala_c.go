package grammarblobs

import _ "embed"

//go:embed scala_c.bin
var blobScalaC []byte

func ScalaC() []byte { return blobScalaC }
