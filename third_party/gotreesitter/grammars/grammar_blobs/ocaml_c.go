package grammarblobs

import _ "embed"

//go:embed ocaml_c.bin
var blobOcamlC []byte

func OcamlC() []byte { return blobOcamlC }
