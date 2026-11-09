package grammarblobs

import _ "embed"

//go:embed rust_c.bin
var blobRustC []byte

func RustC() []byte { return blobRustC }
