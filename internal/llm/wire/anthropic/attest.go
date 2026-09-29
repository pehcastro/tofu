package anthropic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"unicode/utf16"
)

type Attestation int

const (
	AttestationPatched Attestation = iota
	AttestationNoBillingHeader
	AttestationUnanchored
)

func (a Attestation) String() string {
	switch a {
	case AttestationPatched:
		return "patched"
	case AttestationNoBillingHeader:
		return "no_billing_header"
	case AttestationUnanchored:
		return "unanchored"
	}
	panic("anthropic: unknown attestation " + strconv.Itoa(int(a)))
}

func BillingSystemBlock(firstUserMessage, version string) string {
	units := utf16.Encode([]rune(firstUserMessage))
	sources := BillingFingerprintSourceIndexes()
	picked := make([]rune, 0, len(sources))
	for _, index := range sources {
		if index >= len(units) {
			picked = append(picked, '0')
			continue
		}
		picked = append(picked, utf16.Decode(units[index:index+1])...)
	}
	sum := sha256.Sum256([]byte(BillingFingerprintSalt + string(picked) + version))
	suffix := hex.EncodeToString(sum[:])[:BillingFingerprintHexChars]

	return BillingHeaderPrefix + " cc_version=" + version + "." + suffix +
		"; cc_entrypoint=cli; " + BillingCheckPlaceholder + ";"
}

func PatchBillingCheck(body []byte) Attestation {
	anchor := bytes.Index(body, []byte(BillingAnchor))
	if anchor < 0 {
		return AttestationNoBillingHeader
	}
	searchFrom := anchor + len(BillingAnchor)
	offset := bytes.Index(body[searchFrom:], []byte(BillingCheckPlaceholder))
	if offset < 0 || offset > BillingCheckAnchorWindow {
		return AttestationUnanchored
	}

	copy(body[searchFrom+offset+len("cch="):], BillingCheckValue(body))
	return AttestationPatched
}

func BillingCheckValue(body []byte) string {
	return fmt.Sprintf("%0*x", BillingCheckHexChars, xxHash64(body, BillingCheckSeed)&0xfffff)
}
