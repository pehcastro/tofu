package codex

import (
	"cmp"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"boji/internal/transport"
)

const (
	Name               = "codex"
	WatchdogSeconds    = 600
	RedactedCredential = "<redacted>"
	ErrorDetailBytes   = 512
)

type TokenSource func(ctx context.Context) (string, error)

type Config struct {
	BaseURL        string
	Model          string
	Token          TokenSource
	HTTP           *http.Client
	Watchdog       time.Duration
	InstallationID string
	SessionID      string
}

type Wire struct {
	config Config
	http   *http.Client
}

func New(config Config) (*Wire, error) {
	if config.Model == "" {
		return nil, transport.Fail("codex.New", transport.KindBadRequest, nil, "the wire has no model")
	}
	if config.Token == nil {
		return nil, transport.Fail("codex.New", transport.KindMissingCredential, nil, "the wire has no token source")
	}
	if config.Watchdog <= 0 {
		config.Watchdog = WatchdogSeconds * time.Second
	}
	client := config.HTTP
	if client == nil {
		client = &http.Client{}
	}
	return &Wire{config: config, http: client}, nil
}

type Dump struct {
	Method       string
	URL          string
	Headers      []Header
	Body         []byte
	Subscription bool
	Plan         string
}

func (d Dump) String() string {
	var out strings.Builder
	out.WriteString(d.Method + " " + d.URL + "\n")
	for _, header := range d.Headers {
		out.WriteString(header.Name + ": " + redactHeaderValue(header.Name, header.Value) + "\n")
	}
	out.WriteString("\n")
	out.Write(d.Body)
	out.WriteString("\n")
	return out.String()
}

func (w *Wire) Ask(ctx context.Context, request Request) (Result, Dump, error) {
	request.Model = cmp.Or(request.Model, w.config.Model)
	request.Identity.InstallationID = cmp.Or(request.Identity.InstallationID, w.config.InstallationID)
	request.Identity.SessionID = cmp.Or(request.Identity.SessionID, w.config.SessionID)

	token, err := w.config.Token(ctx)
	if err != nil {
		return Result{}, Dump{}, err
	}
	if token == "" {
		return Result{}, Dump{}, transport.Fail("codex.Ask", transport.KindMissingCredential, nil,
			"the token source returned nothing")
	}
	claims := ReadClaims(token)
	subscription := claims.Subscription()

	identity, err := request.Identity.filled()
	if err != nil {
		return Result{}, Dump{}, err
	}
	metadataHeader, clientMetadata, err := identity.TurnMetadata()
	if err != nil {
		return Result{}, Dump{}, err
	}
	if !subscription {
		clientMetadata = nil
	}

	request.Identity = identity
	body, err := request.Encode(clientMetadata)
	if err != nil {
		return Result{}, Dump{}, err
	}

	dump := Dump{
		Method:       http.MethodPost,
		URL:          w.endpoint(subscription),
		Subscription: subscription,
		Plan:         claims.PlanType,
		Headers: Headers(HeaderOptions{
			Token:        token,
			Subscription: subscription,
			Claims:       claims,
			Model:        request.Model,
			ServiceTier:  request.ServiceTier,
			Identity:     identity,
			TurnMetadata: metadataHeader,
			TurnState:    request.TurnState,
		}),
		Body: body,
	}

	result, err := w.post(ctx, dump)
	if refused := request.RefusedControls(); len(refused) > 0 {
		result.Warnings = append([]string{
			"the codex backend refuses these and they were dropped: " + strings.Join(refused, ", "),
		}, result.Warnings...)
	}
	return result, dump, err
}

func (w *Wire) endpoint(subscription bool) string {
	if w.config.BaseURL != "" {
		return strings.TrimRight(w.config.BaseURL, "/")
	}
	if subscription {
		return SubscriptionBaseURL + SubscriptionPath
	}
	return KeyBaseURL + KeyPath
}

func (w *Wire) post(ctx context.Context, dump Dump) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, w.config.Watchdog)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, dump.Method, dump.URL, strings.NewReader(string(dump.Body)))
	if err != nil {
		return Result{}, transport.Fail("codex.Ask", transport.KindBadRequest, err, "building the request")
	}
	for _, header := range dump.Headers {
		request.Header[header.Name] = []string{header.Value}
	}

	response, err := w.http.Do(request)
	if err != nil {
		return Result{}, transport.Fail("codex.Ask", transport.KindProvider, err, "posting the request")
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, ErrorDetailBytes))
		return Result{}, &transport.Error{
			Kind:   statusKind(response.StatusCode),
			Op:     "codex.Ask",
			Status: response.StatusCode,
			Detail: strings.TrimSpace(string(detail)),
		}
	}

	result, err := ReadStream(response.Body)
	if turnState := response.Header.Get(HeaderTurnState); turnState != "" {
		result.TurnState = turnState
	}
	return result, err
}

func statusKind(status int) transport.Kind {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return transport.KindAuth
	case status == http.StatusPaymentRequired:
		return transport.KindBilling
	case status == http.StatusNotFound:
		return transport.KindModelAccess
	case status == http.StatusRequestEntityTooLarge:
		return transport.KindRequestTooLarge
	case status == http.StatusTooManyRequests:
		return transport.KindRateLimit
	case status >= 500:
		return transport.KindProvider
	}
	return transport.KindBadRequest
}
