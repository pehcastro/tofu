package grammarblobs

import _ "embed"

//go:embed zig_c.bin
var blobZigC []byte

func ZigC() []byte { return blobZigC }
