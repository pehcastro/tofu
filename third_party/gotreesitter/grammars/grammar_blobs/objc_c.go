package grammarblobs

import _ "embed"

//go:embed objc_c.bin
var blobObjcC []byte

func ObjcC() []byte { return blobObjcC }
