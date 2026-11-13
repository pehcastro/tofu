package grammarblobs

import _ "embed"

//go:embed toml_c.bin
var blobTomlC []byte

func TomlC() []byte { return blobTomlC }
