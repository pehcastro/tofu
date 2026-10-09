package jev

import (
	"context"
	"strconv"

	"tofu/internal/transport"
)

func failsOver(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	switch kind := transport.KindOf(err); kind {
	case transport.KindBilling, transport.KindBudget, transport.KindRateLimit, transport.KindAuth,
		transport.KindModelAccess, transport.KindTimeout, transport.KindProvider:
		return true
	case transport.KindUnknown, transport.KindMissingCredential, transport.KindRequestTooLarge,
		transport.KindBadRequest, transport.KindInvalidAnswer:
		return false
	default:
		panic("jev: unknown transport kind " + strconv.Itoa(int(kind)))
	}
}
