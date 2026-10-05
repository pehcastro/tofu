package transport

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
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

func StatusKind(status int) Kind {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return KindAuth
	case http.StatusPaymentRequired:
		return KindBilling
	case http.StatusNotFound:
		return KindModelAccess
	case http.StatusRequestTimeout:
		return KindTimeout
	case http.StatusRequestEntityTooLarge:
		return KindRequestTooLarge
	case http.StatusTooManyRequests:
		return KindRateLimit
	}
	if status >= 500 {
		return KindProvider
	}
	return KindBadRequest
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

var ErrContextOverflow = errors.New("the request does not fit the model's context window")

func ContextOverflow(err error) bool {
	if errors.Is(err, ErrContextOverflow) {
		return true
	}
	var failure *Error
	if !errors.As(err, &failure) || failure.Kind != KindBadRequest && failure.Kind != KindRequestTooLarge {
		return false
	}
	said := strings.ToLower(failure.Detail)
	mentions := func(phrases ...string) bool {
		return slices.ContainsFunc(phrases, func(phrase string) bool { return strings.Contains(said, phrase) })
	}
	if mentions("rate limit", "rate_limit", "too many requests") {
		return false
	}
	return failure.Status == http.StatusRequestEntityTooLarge || mentions("prompt is too long", "request_too_large", "exceeds the context window",
		"maximum context length", "context_length_exceeded", "context length exceeded", "model_context_window_exceeded", "too many tokens", "token limit exceeded")
}

func Fail(op string, kind Kind, cause error, format string, args ...any) *Error {
	return &Error{Kind: kind, Op: op, Detail: fmt.Sprintf(format, args...), Err: cause}
}
