package codex

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"strings"
)

type Claims struct {
	AccountID string
	PlanType  string
	Residency string
}

type authClaim struct {
	AccountID        string `json:"chatgpt_account_id"`
	PlanType         string `json:"chatgpt_plan_type"`
	DataResidency    string `json:"chatgpt_data_residency"`
	ComputeResidency string `json:"chatgpt_compute_residency"`
}

func ReadClaims(token string) Claims {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}
	}
	payload, err := decodeSegment(parts[1])
	if err != nil {
		return Claims{}
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(payload, &body) != nil {
		return Claims{}
	}
	var auth authClaim
	if json.Unmarshal(body[JWTAuthClaim], &auth) != nil {
		return Claims{}
	}
	return Claims{
		AccountID: strings.TrimSpace(auth.AccountID),
		PlanType:  strings.TrimSpace(auth.PlanType),
		Residency: cmp.Or(strings.TrimSpace(auth.DataResidency), strings.TrimSpace(auth.ComputeResidency)),
	}
}

func (c Claims) Subscription() bool {
	return c.AccountID != ""
}

func decodeSegment(segment string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimRight(segment, "="))
	if padding := len(normalized) % 4; padding != 0 {
		normalized += strings.Repeat("=", 4-padding)
	}
	return base64.StdEncoding.DecodeString(normalized)
}
