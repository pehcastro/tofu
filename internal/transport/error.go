package transport

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	resetLayout = "Mon 15:04"
	EmptyAnswer = "empty_answer"
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

type Account struct {
	Source string
	Label  string
	Plan   string
	SignIn string
}

type Error struct {
	Kind      Kind
	Op        string
	Status    int
	RequestID string
	Detail    string
	Err       error
	Code      string
	Said      string
	ResetAt   time.Time
	Account   Account
}

func (e *Error) Error() string {
	if e.Status == 0 && e.Code == "" {
		return e.technical()
	}
	return e.Sentence() + "\n" + e.technical()
}

func Sentenced(text string) (string, bool) {
	sentence, technical, found := strings.Cut(text, "\n")
	op, said, opened := strings.Cut(technical, ": ")
	if !found || !opened || op == "" || strings.Contains(op, " ") {
		return "", false
	}
	for kind := KindUnknown; kind <= KindProvider; kind++ {
		if name := kind.String(); said == name || strings.HasPrefix(said, name+" ") || strings.HasPrefix(said, name+":") {
			return sentence, true
		}
	}
	return "", false
}

func (e *Error) Sentence() string {
	who := cmp.Or(e.Account.Source, strings.Split(e.Op, ".")[0])
	if e.Account.Label != "" {
		who += " account " + e.Account.Label
	}
	if e.Account.Plan != "" {
		who += " on the " + e.Account.Plan + " plan"
	}
	answered := e.Code
	if e.Status != 0 {
		answered = strings.TrimSpace(strconv.Itoa(e.Status) + " " + e.Code)
	}
	if e.Said != "" {
		answered += ": " + e.Said
	}
	signIn := "Sign in again"
	if e.Account.SignIn != "" {
		signIn += " with " + e.Account.SignIn
	}
	const pick, picking = "pick another model with Ctrl+L", "Pick another model with Ctrl+L"
	switch e.Kind {
	case KindAuth:
		if e.Status == http.StatusForbidden {
			return who + " refused this request (" + answered + "): this account or its organization may not use this model this way. " + picking + ", or sign in with another account."
		}
		return who + " rejected the sign-in (" + answered + "): the token expired or was revoked. " + signIn + ", or " + pick + "."
	case KindMissingCredential:
		return who + " has no sign-in (" + answered + "). " + signIn + ", or " + pick + "."
	case KindBilling:
		return who + " refused for billing (" + answered + "): this account has no billing set up or no credit left. Set up billing with the vendor, or " + pick + "."
	case KindBudget:
		return who + " reached its spend limit (" + answered + "). Raise the limit with the vendor, or " + pick + "."
	case KindModelAccess:
		return who + " does not offer this model (" + answered + "). " + picking + "."
	case KindRateLimit:
		if e.ResetAt.IsZero() {
			return who + " is at its usage limit (" + answered + "). Wait for its window to reset, or " + pick + "."
		}
		return who + " is at its usage limit (" + answered + "), which resets at " + e.ResetAt.Local().Format(resetLayout) + ". Wait until then, or " + pick + "."
	case KindRequestTooLarge:
		return who + " refused the request as too large (" + answered + "). Run /compact, or start a new session."
	case KindInvalidAnswer:
		if e.Code == EmptyAnswer {
			return who + " answered with nothing (" + answered + "). Send it again, or " + pick + "."
		}
		return who + " answered with something tofu could not read (" + answered + "). Send it again, or " + pick + "."
	case KindBadRequest, KindUnknown:
		return who + " refused the request (" + answered + "). Send it again, or " + pick + "."
	case KindTimeout, KindProvider:
		return who + " failed on its side (" + answered + "). Send it again in a moment, or " + pick + "."
	}
	panic("transport: unknown error kind " + strconv.Itoa(int(e.Kind)))
}

func (e *Error) technical() string {
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
