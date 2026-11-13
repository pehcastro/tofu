package grammarblobs

import _ "embed"

//go:embed proto_c.bin
var blobProtoC []byte

func ProtoC() []byte { return blobProtoC }
