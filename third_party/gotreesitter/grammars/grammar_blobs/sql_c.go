package grammarblobs

import _ "embed"

//go:embed sql_c.bin
var blobSqlC []byte

func SqlC() []byte { return blobSqlC }
