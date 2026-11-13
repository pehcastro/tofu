package grammarblobs

import _ "embed"

//go:embed erlang_c.bin
var blobErlangC []byte

func ErlangC() []byte { return blobErlangC }
