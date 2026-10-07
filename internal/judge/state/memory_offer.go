package state

import "tofu/internal/judge/ledger"

const MemoryOfferPoint = "memory_offer"

type MemoryOfferState struct {
	Message string `json:"message"`
}

func BuildMemoryOffer(in MemoryOfferState) ([]byte, string, error) {
	canon, err := ledger.Canonical(in)
	return canon, deriveVersion(MemoryOfferPoint, MemoryOfferState{}), err
}
