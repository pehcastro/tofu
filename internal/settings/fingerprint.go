package settings

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"tofu/internal/llm/wire/anthropic"
	"tofu/internal/llm/wire/codex"
	"tofu/internal/transport"
)

const (
	SubFingerprint        = "subFingerprint"
	FingerprintClaudeCode = "claudeCode"
	FingerprintCodex      = "codex"
	npmRegistryURL        = "https://registry.npmjs.org"
	NpmRegistryVariable   = "TOFU_NPM_REGISTRY_URL"
)

type Fingerprint struct {
	ClaudeCode string `json:"claudeCode"`
	Codex      string `json:"codex"`
}

func PinnedFingerprint() Fingerprint {
	return Fingerprint{ClaudeCode: anthropic.PinnedClaudeCodeVersion, Codex: codex.PinnedCodexClientVersion}
}

func (s *Store) Fingerprint() Fingerprint {
	var declared Fingerprint
	_ = json.Unmarshal(s.objects[Global][SubFingerprint], &declared)
	pinned := PinnedFingerprint()
	return Fingerprint{
		ClaudeCode: anthropic.NewerVersion(pinned.ClaudeCode, declared.ClaudeCode),
		Codex:      anthropic.NewerVersion(pinned.Codex, declared.Codex),
	}
}

type RaisedVersion struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

func NpmRegistry() string {
	return cmp.Or(strings.TrimSpace(os.Getenv(NpmRegistryVariable)), npmRegistryURL)
}

func LatestFingerprint(ctx context.Context, client *transport.Client, registry string) (Fingerprint, error) {
	var latest Fingerprint
	for pkg, version := range map[string]*string{anthropic.ClaudeCodePackage: &latest.ClaudeCode, codex.CodexPackage: &latest.Codex} {
		response, err := client.Do(ctx, transport.Request{Method: http.MethodGet, URL: registry + "/" + pkg + "/latest", Header: http.Header{"Accept": []string{"application/json"}}})
		if err != nil {
			return Fingerprint{}, err
		}
		var release struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(response.Body, &release); err != nil {
			return Fingerprint{}, fmt.Errorf("%s: %w", pkg, err)
		}
		*version = release.Version
	}
	return latest, nil
}

func (s *Store) RaiseFingerprint(latest Fingerprint) ([]RaisedVersion, error) {
	held := s.Fingerprint()
	var raised []RaisedVersion
	for _, offer := range []RaisedVersion{{FingerprintClaudeCode, held.ClaudeCode, latest.ClaudeCode}, {FingerprintCodex, held.Codex, latest.Codex}} {
		if anthropic.NewerVersion(offer.From, offer.To) == offer.From {
			continue
		}
		if err := s.saveFingerprint(Global, offer.Key, offer.To); err != nil {
			return raised, err
		}
		raised = append(raised, offer)
	}
	return raised, nil
}

func (s *Store) saveFingerprint(scope Scope, key, version string) error {
	if err := s.reread(scope); err != nil {
		return err
	}
	held := map[string]any{}
	_ = json.Unmarshal(s.objects[scope][SubFingerprint], &held)
	held[key] = version
	encoded, err := json.Marshal(held)
	if err != nil {
		return err
	}
	s.objects[scope][SubFingerprint] = encoded
	return s.write(scope)
}
