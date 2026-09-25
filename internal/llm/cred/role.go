package cred

import (
	"fmt"
	"strings"
)

type Role string

const (
	JevProvider Role = "jev-provider"
	LLMProvider Role = "llm-provider"
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
		return JevProvider, nil
	}
	return "", fmt.Errorf("cred: %q is not a registered key vendor", vendor)
}

func NewJevKey(vendor Vendor, secret string) (JevKey, error) {
	role, err := roleOf(vendor)
	if err != nil {
		return JevKey{}, err
	}
	if role != JevProvider {
		return JevKey{}, fmt.Errorf("cred: %s is registered as a %s and may not buy a typed decision", vendor, role)
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
	if role != LLMProvider {
		return LLMKey{}, fmt.Errorf("cred: %s is registered as a %s and may not send a prompt", vendor, role)
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return LLMKey{}, fmt.Errorf("cred: the %s key is empty", vendor)
	}
	return LLMKey{secret: secret}, nil
}

func (k LLMKey) Prompt() string { return k.secret }
