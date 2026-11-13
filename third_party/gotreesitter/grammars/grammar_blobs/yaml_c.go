package grammarblobs

import _ "embed"

//go:embed yaml_c.bin
var blobYamlC []byte

func YamlC() []byte { return blobYamlC }
