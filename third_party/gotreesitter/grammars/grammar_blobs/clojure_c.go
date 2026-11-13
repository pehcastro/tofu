package grammarblobs

import _ "embed"

//go:embed clojure_c.bin
var blobClojureC []byte

func ClojureC() []byte { return blobClojureC }
