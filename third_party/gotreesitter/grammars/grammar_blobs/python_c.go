package grammarblobs

import _ "embed"

//go:embed python_c.bin
var blobPythonC []byte

func PythonC() []byte { return blobPythonC }
