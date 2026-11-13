package grammarblobs

import _ "embed"

//go:embed dart_c.bin
var blobDartC []byte

func DartC() []byte { return blobDartC }
