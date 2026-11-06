package main

import (
	"tofu/internal/judge/ledger"
	"tofu/internal/models/classifiers"
	jev "tofu/internal/models/classifiers/core"
	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
	"tofu/internal/tools/agents/claims"
	"tofu/internal/tools/contract"
)

func newClaimsTool(dir string) (contract.Tool, error) {
	sessions, err := session.OpenIn(dir)
	if err != nil {
		return nil, err
	}
	places, err := platformproject.ResolvePlaces(dir)
	if err != nil {
		return nil, err
	}
	return claims.Claims{
		Sessions: sessions,
		Dir:      dir,
		Ledger:   ledger.NewWriter(places.Log),
		Open:     func() (*jev.Client, error) { return classifiers.Open(dir, jev.ReadKeyring(), 1) },
	}, nil
}
