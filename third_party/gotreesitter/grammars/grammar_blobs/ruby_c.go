package grammarblobs

import _ "embed"

//go:embed ruby_c.bin
var blobRubyC []byte

func RubyC() []byte { return blobRubyC }
