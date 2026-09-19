package anthropic

import (
	"strings"
	"testing"
)

func TestXXHash64MatchesThePublishedVectors(t *testing.T) {
	if got := xxHash64(nil, 0); got != 0xef46db3751d8e999 {
		t.Fatalf("xxhash64(\"\", 0) is %#x", got)
	}
	if got := xxHash64([]byte("abc"), 0); got != 0x44bc2cf5ad770999 {
		t.Fatalf("xxhash64(\"abc\", 0) is %#x", got)
	}
	if got := xxHash64([]byte("Nobody inspects the spammish repetition"), 0); got != 0xfbcea83c8a378bf1 {
		t.Fatalf("xxhash64(long, 0) is %#x", got)
	}
	if got := xxHash64([]byte("abc"), 0x4d659218e32a3268); got == xxHash64([]byte("abc"), 0) {
		t.Fatalf("the seed is being ignored: %#x", got)
	}
}

func TestBillingCheckMatchesTheReferenceLowTwentyBits(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"cch=00000", "a47f7"},
		{`{"messages":[],"cch=00000","x":1}`, "3073d"},
		{"x-anthropic-billing-header: cc_version=2.1.158; cc_entrypoint=cli; cch=00000;", "f2b0b"},
	}
	for _, one := range cases {
		if got := BillingCheckValue([]byte(one.body)); got != one.want {
			t.Fatalf("body %q hashed to %q, want %q", one.body, got, one.want)
		}
	}
}

func TestBillingSystemBlockCarriesThePlaceholderAndTheVersionSuffix(t *testing.T) {
	block := BillingSystemBlock("hello world, this is the first user message")
	if !strings.HasPrefix(block, BillingHeaderPrefix+" cc_version="+PinnedClaudeCodeVersion+".") {
		t.Fatalf("block is %q", block)
	}
	if !strings.HasSuffix(block, "; cc_entrypoint=cli; "+BillingCheckPlaceholder+";") {
		t.Fatalf("block is %q", block)
	}
	suffix := strings.TrimPrefix(block, BillingHeaderPrefix+" cc_version="+PinnedClaudeCodeVersion+".")
	suffix = suffix[:BillingFingerprintHexChars]
	if BillingSystemBlock("hellx wxrld, this is the fixst user message") == block {
		t.Fatal("the fingerprint ignored the characters it is built from")
	}
	if BillingSystemBlock("qqqqoqqoqqqqqqqqqqqq scrambled tail") != block {
		t.Fatalf("the fingerprint read characters outside indexes 4, 7 and 20; suffix %q", suffix)
	}
}

func TestBillingSystemBlockPadsAShortMessage(t *testing.T) {
	if BillingSystemBlock("") != BillingSystemBlock("000") {
		t.Fatal("an absent character must read as \"0\"")
	}
}

func TestPatchBillingCheckWritesTheCheckOverThePlaceholder(t *testing.T) {
	body := []byte(`{"model":"m","messages":[],"system":[{"type":"text","text":"` +
		BillingHeaderPrefix + ` cc_version=2.1.257.abc; cc_entrypoint=cli; cch=00000;"}]}`)
	want := BillingCheckValue(body)

	if state := PatchBillingCheck(body); state != AttestationPatched {
		t.Fatalf("state is %s", state)
	}
	if !strings.Contains(string(body), "cch="+want+";") {
		t.Fatalf("patched body is %s", body)
	}
	if strings.Contains(string(body), BillingCheckPlaceholder) {
		t.Fatalf("the placeholder survived: %s", body)
	}
}

func TestPatchBillingCheckReportsAMissingAnchor(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"cch=00000"}]}`)
	if state := PatchBillingCheck(body); state != AttestationNoBillingHeader {
		t.Fatalf("state is %s", state)
	}
	if !strings.Contains(string(body), BillingCheckPlaceholder) {
		t.Fatalf("a bare literal in user content was patched: %s", body)
	}
}

func TestPatchBillingCheckReportsAPlaceholderOutOfRangeOfTheAnchor(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"` + BillingHeaderPrefix + " " +
		strings.Repeat("p", BillingCheckAnchorWindow+1) + ` cch=00000;"}]}`)
	if state := PatchBillingCheck(body); state != AttestationUnanchored {
		t.Fatalf("state is %s", state)
	}
}
