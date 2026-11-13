package grammarblobs

import _ "embed"

//go:embed cpp_c.bin
var blobCppC []byte

func CppC() []byte { return blobCppC }
