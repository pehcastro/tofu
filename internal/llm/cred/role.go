package cred

import (
	"fmt"
	"strings"
)

type Role string

const (
	RoleClassifier Role = "classifier"
	RoleLLM        Role = "llm"
)

type Vendor string

const (
	OpenRouter Vendor = "openrouter"
	TypeSafe   Vendor = "typesafe"
)

type JevKey struct {
	secret string
}

type LLMKey struct {
	secret string
}

func roleOf(vendor Vendor) (Role, error) {
	switch vendor {
	case OpenRouter, TypeSafe:
		return RoleClassifier, nil
	}
	return "", fmt.Errorf("cred: %q is not a registered key vendor", vendor)
}

func NewJevKey(vendor Vendor, secret string) (JevKey, error) {
	role, err := roleOf(vendor)
	if err != nil {
		return JevKey{}, err
	}
	if role != RoleClassifier {
		return JevKey{}, fmt.Errorf("cred: %s asked for a %s credential, %s is registered as %s", vendor, RoleClassifier, vendor, role)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return JevKey{}, fmt.Errorf("cred: the %s key is empty", vendor)
	}
	return JevKey{secret: secret}, nil
}

func NewLLMKey(vendor Vendor, secret string) (LLMKey, error) {
	role, err := roleOf(vendor)
	if err != nil {
		return LLMKey{}, err
	}
	if role != RoleLLM {
		return LLMKey{}, fmt.Errorf("cred: %s asked for a %s credential, %s is registered as %s", vendor, RoleLLM, vendor, role)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return LLMKey{}, fmt.Errorf("cred: the %s key is empty", vendor)
	}
	return LLMKey{secret: secret}, nil
}

func (k LLMKey) Prompt() string { return k.secret }
