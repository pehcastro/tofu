package jev

import (
	"strconv"

	"boji/internal/transport"
)

type Unavailable int

const (
	UnavailableTimeout Unavailable = iota
	UnavailableRefused
	UnavailableRateLimit
	UnavailableTransport
	UnavailableMalformedAnswer
)

func (u Unavailable) String() string {
	switch u {
	case UnavailableTimeout:
		return "timeout"
	case UnavailableRefused:
		return "refused"
	case UnavailableRateLimit:
		return "rate_limit"
	case UnavailableTransport:
		return "transport"
	case UnavailableMalformedAnswer:
		return "malformed_answer"
	}
	panic("jev: unknown unavailable reason " + strconv.Itoa(int(u)))
}

func Unavailability(err error) Unavailable {
	switch transport.KindOf(err) {
	case transport.KindTimeout:
		return UnavailableTimeout
	case transport.KindRateLimit:
		return UnavailableRateLimit
	case transport.KindInvalidAnswer:
		return UnavailableMalformedAnswer
	case transport.KindMissingCredential, transport.KindAuth, transport.KindBilling,
		transport.KindModelAccess, transport.KindBudget, transport.KindRequestTooLarge,
		transport.KindBadRequest:
		return UnavailableRefused
	case transport.KindUnknown, transport.KindProvider:
		return UnavailableTransport
	}
	panic("jev: unknown transport kind")
}
