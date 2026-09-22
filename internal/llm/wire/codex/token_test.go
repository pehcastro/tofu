package codex

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"tofu/internal/llm/cred"
)

func TestTheCredentialSpecReadsTheAccountClaimThisPackageNames(t *testing.T) {
	spec, err := cred.Lookup(string(cred.Codex))
	if err != nil {
		t.Fatalf("looking up the codex credential spec: %v", err)
	}
	if want := JWTAuthClaim + "." + AccountClaim; spec.AccountIDPath != want {
		t.Fatalf("the credential spec reads the account from %q and this package names %q", spec.AccountIDPath, want)
	}
	if spec.IdentityTokenField == "" {
		t.Fatal("the codex credential spec names no token to read the identity from")
	}
}

func fakeJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("encoding the claims: %v", err)
	}
	segment := base64.RawURLEncoding.EncodeToString
	return segment([]byte(`{"alg":"none"}`)) + "." + segment(payload) + ".signature-not-checked"
}

func subscriptionToken(t *testing.T) string {
	t.Helper()
	return fakeJWT(t, map[string]any{
		JWTAuthClaim: map[string]any{
			AccountClaim:       "acct-0000",
			PlanClaim:          "pro",
			DataResidencyClaim: "eu",
		},
	})
}

func TestClaimsChooseTheBranch(t *testing.T) {
	claims := ReadClaims(subscriptionToken(t))
	if !claims.Subscription() {
		t.Fatal("a token carrying the account claim did not take the subscription branch")
	}
	if claims.AccountID != "acct-0000" || claims.PlanType != "pro" || claims.Residency != "eu" {
		t.Fatalf("claims read as %+v", claims)
	}

	withoutAccount := fakeJWT(t, map[string]any{
		JWTAuthClaim: map[string]any{PlanClaim: "pro"},
		"sub":        "user-0000",
	})
	if ReadClaims(withoutAccount).Subscription() {
		t.Fatal("a token with no account claim took the subscription branch")
	}
	if ReadClaims(withoutAccount).PlanType != "pro" {
		t.Fatal("the plan claim is not read when the account claim is absent")
	}

	for name, token := range map[string]string{
		"an api key":      "sk-proj-0000",
		"two segments":    "aaa.bbb",
		"unreadable body": "aaa.!!!!.ccc",
		"empty":           "",
	} {
		if ReadClaims(token).Subscription() {
			t.Fatalf("%s took the subscription branch", name)
		}
	}
}

func TestResidencyFallsBackToCompute(t *testing.T) {
	token := fakeJWT(t, map[string]any{
		JWTAuthClaim: map[string]any{
			AccountClaim:          "acct-0000",
			DataResidencyClaim:    "   ",
			ComputeResidencyClaim: "us",
		},
	})
	if got := ReadClaims(token).Residency; got != "us" {
		t.Fatalf("residency is %q, want the compute claim", got)
	}
}
