package grammarblobs

import _ "embed"

//go:embed elixir_c.bin
var blobElixirC []byte

func ElixirC() []byte { return blobElixirC }
