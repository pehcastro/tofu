package wiring

import (
	"tofu/internal/models/classifiers/core"
	"tofu/internal/platform/credential"
)

const SetAsideCause = "set aside by hand, run tofu login --enable to bring it back"

func ResolvedKey(variable string) string {
	key, _ := core.KeyFor(credential.CredentialFileName, variable)
	return key
}
