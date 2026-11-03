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

func NewJevClient() (*jev.Client, error) {
	return JevClientFrom(jev.ReadKeyring(), OneCallAtATime)
}

func JevClientFrom(keys jev.Keyring, concurrency int) (*jev.Client, error) {
	return classifier.Open(".", keys, concurrency)
}

func BoundClassifier() (models.Model, error) {
	return classifier.Bound(".", jev.ReadKeyring())
}

func LocateGateKey(keys jev.Keyring) (jev.Located, error) {
	bound, err := classifier.Bound(".", keys)
	if err != nil {
		return jev.Located{}, err
	}
	return keys.LocateFor(sys.CredentialFileName, classifier.KeyName(bound.Provider))
}

func JevClientOn(key string, concurrency int) (*jev.Client, error) {
	model, err := BoundClassifier()
	if err != nil {
		return nil, err
	}
	return classifier.Route{Model: model, Key: key}.Client(concurrency)
}

func GateKey() (string, error) {
	return GateKeyFrom(jev.ReadKeyring())
}

func GateKeyFrom(keys jev.Keyring) (string, error) {
	route, err := classifier.Resolve(".", keys)
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
