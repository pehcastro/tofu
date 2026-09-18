package transport

import (
	"errors"
	"fmt"
	"strconv"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindMissingCredential
	KindAuth
	KindBilling
	KindModelAccess
	KindBudget
	KindRequestTooLarge
	KindBadRequest
	KindRateLimit
	KindTimeout
	KindInvalidAnswer
	KindProvider
)

func (k Kind) Fatal() bool {
	switch k {
	case KindRateLimit, KindTimeout, KindInvalidAnswer, KindProvider:
		return false
	case KindUnknown, KindMissingCredential, KindAuth, KindBilling,
		KindModelAccess, KindBudget, KindRequestTooLarge, KindBadRequest:
		return true
	}
	panic("transport: unknown error kind " + strconv.Itoa(int(k)))
}

func (k Kind) String() string {
	switch k {
	case KindUnknown:
		return "unknown"
	case KindMissingCredential:
		return "missing_credential"
	case KindAuth:
		return "auth"
	case KindBilling:
		return "billing"
	case KindModelAccess:
		return "model_access"
	case KindBudget:
		return "budget"
	case KindRequestTooLarge:
		return "request_too_large"
	case KindBadRequest:
		return "bad_request"
	case KindRateLimit:
		return "rate_limit"
	case KindTimeout:
		return "timeout"
	case KindInvalidAnswer:
		return "invalid_answer"
	case KindProvider:
		return "provider"
	}
	panic("transport: unknown error kind " + strconv.Itoa(int(k)))
}

type Error struct {
	Kind      Kind
	Op        string
	Status    int
	RequestID string
	Detail    string
	Err       error
}

func (e *Error) Error() string {
	text := e.Op + ": " + e.Kind.String()
	if e.Status != 0 {
		text += " " + strconv.Itoa(e.Status)
	}
	if e.RequestID != "" {
		text += " [" + e.RequestID + "]"
	}
	if e.Detail != "" {
		text += ": " + e.Detail
	}
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	return text
}

func (e *Error) Unwrap() error { return e.Err }

func KindOf(err error) Kind {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Kind
	}
	return KindUnknown
}

func Fail(op string, kind Kind, cause error, format string, args ...any) *Error {
	return &Error{Kind: kind, Op: op, Detail: fmt.Sprintf(format, args...), Err: cause}
}
