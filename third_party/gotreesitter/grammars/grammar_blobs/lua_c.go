package grammarblobs

import _ "embed"

//go:embed lua_c.bin
var blobLuaC []byte

func LuaC() []byte { return blobLuaC }
