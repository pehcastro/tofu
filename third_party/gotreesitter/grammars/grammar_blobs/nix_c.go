package grammarblobs

import _ "embed"

//go:embed nix_c.bin
var blobNixC []byte

func NixC() []byte { return blobNixC }
