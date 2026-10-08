package memory

import _ "embed"

//go:embed scopes@1.yaml
var scopes []byte

func Scopes() []byte { return scopes }
