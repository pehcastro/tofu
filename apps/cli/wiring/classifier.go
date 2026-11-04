package wiring

import (
	"errors"
	"strconv"

	"tofu/apps/cli/output"
	"tofu/internal/host"
	"tofu/internal/models/classifiers"
	"tofu/internal/models/classifiers/core"
	"tofu/internal/models/llm/catalog"
	"tofu/internal/models/transport"
	"tofu/internal/sys"
)

const OneCallAtATime = 1

func NewJevClient() (*core.Client, error) {
	return JevClientFrom(core.ReadKeyring(), OneCallAtATime)
}

func JevClientFrom(keys core.Keyring, concurrency int) (*core.Client, error) {
	return classifiers.Open(".", keys, concurrency)
}

func BoundClassifier() (catalog.Model, error) {
	return classifiers.Bound(".", core.ReadKeyring())
}

func LocateGateKey(keys core.Keyring) (core.Located, error) {
	bound, err := classifiers.Bound(".", keys)
	if err != nil {
		return core.Located{}, err
	}
	return keys.LocateFor(sys.CredentialFileName, classifiers.KeyName(bound.Provider))
}

func JevClientOn(key string, concurrency int) (*core.Client, error) {
	model, err := BoundClassifier()
	if err != nil {
		return nil, err
	}
	return classifiers.Route{Model: model, Key: key}.Client(concurrency)
}

func GateKey() (string, error) {
	return GateKeyFrom(core.ReadKeyring())
}

func GateKeyFrom(keys core.Keyring) (string, error) {
	route, err := classifiers.Resolve(".", keys)
	return route.Key, err
}

func CommandFor(verb string, of host.AccountRole, provider string) string {
	return "tofu " + verb + " " + string(of) + " " + provider
}

func Failed(o output.Verb, err error) int {
	var refused *transport.Error
	switch {
	case !errors.As(err, &refused):
	case refused.Kind == transport.KindMissingCredential:
		err = output.ProblemError{What: refused.Detail, Hint: ClassifierLoginHint()}
	case refused.Status == 0:
	case refused.Kind == transport.KindAuth:
		err = output.ProblemError{What: "jev refused the key (" + strconv.Itoa(refused.Status) + ")", Hint: ClassifierLoginHint()}
	default:
		err = output.ProblemError{What: "jev refused the call (" + strconv.Itoa(refused.Status) + " " + refused.Kind.String() + ")"}
	}
	_ = o.Fail(err)
	return output.ExitUsage
}

func ClassifierLoginHint() string {
	bound, err := BoundClassifier()
	if err != nil {
		return ""
	}
	return CommandFor("login", host.RoleClassifier, string(bound.Provider))
}
