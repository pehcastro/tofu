package anthropic

import (
	"cmp"
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

const (
	Name                    = llm.WireAnthropic
	MessagesPath            = "/v1/messages"
	OAuthQuery              = "?beta=true"
	WatchdogSeconds         = 600
	unattestedRequestNotice = "the billing placeholder is present but not patched; sending an unattested request"
)

type TokenSource func(ctx context.Context) (string, error)

type Config struct {
	BaseURL   string
	Model     string
	Token     TokenSource
	HTTP      *http.Client
	Transport transport.Config
	Watchdog  time.Duration
	Proxy     bool
	SessionID string
	AccountID string
	InstallID string
}

type Wire struct {
	config Config
	http   *http.Client
}

func New(config Config) (*Wire, error) {
	if config.Model == "" {
		return nil, transport.Fail("anthropic.New", transport.KindBadRequest, nil, "the wire has no model")
	}
	if config.Token == nil {
		return nil, transport.Fail("anthropic.New", transport.KindMissingCredential, nil, "the wire has no token source")
	}
	if config.BaseURL == "" {
		config.BaseURL = OfficialBaseURL
	}
	if !IsOfficialBaseURL(config.BaseURL) && !config.Proxy {
		return nil, transport.Fail("anthropic.New", transport.KindMissingCredential, nil,
			"%q is not the official anthropic host and the wire is not configured as a proxy", config.BaseURL)
	}
	if config.Watchdog <= 0 {
		config.Watchdog = WatchdogSeconds * time.Second
	}
	client := &http.Client{}
	if config.HTTP != nil {
		*client = *config.HTTP
	}
	client.Transport = llm.Retrying(client.Transport, config.Transport)
	return &Wire{config: config, http: client}, nil
}

type Dump struct {
	llm.Dump
	Attestation Attestation
}

func (w *Wire) Ask(ctx context.Context, request Request) (Result, Dump, error) {
	request.Model = cmp.Or(request.Model, w.config.Model)
	request.SessionID = cmp.Or(request.SessionID, w.config.SessionID)
	request.AccountID = cmp.Or(request.AccountID, w.config.AccountID)
	request.InstallID = cmp.Or(request.InstallID, w.config.InstallID)

	token, err := w.config.Token(ctx)
	if err != nil {
		return Result{}, Dump{}, err
	}
	if token == "" {
		return Result{}, Dump{}, transport.Fail("anthropic.Ask", transport.KindMissingCredential, nil,
			"the token source returned nothing")
	}
	oauth := IsOAuthToken(token)
	if oauth && request.SessionID == "" {
		if request.SessionID, err = randomUUID(); err != nil {
			return Result{}, Dump{}, err
		}
	}

	body, err := request.Encode(oauth)
	if err != nil {
		return Result{}, Dump{}, err
	}

	var warnings []string
	attestation := AttestationNoBillingHeader
	if oauth {
		attestation = PatchBillingCheck(body)
		if attestation == AttestationUnanchored {
			warnings = append(warnings, unattestedRequestNotice)
		}
	}

	dump := Dump{
		Dump: llm.Dump{
			Method: http.MethodPost,
			URL:    w.endpoint(oauth),
			Headers: Headers(HeaderOptions{
				Token:        token,
				OAuth:        oauth,
				Stream:       true,
				AgentRequest: len(request.Tools) > 0 || request.Thinking,
				Thinking:     request.Thinking,
				SessionID:    request.SessionID,
				ExtraBetas:   request.extraBetas(oauth),
			}),
			Body: body,
			Identifiers: []string{
				request.SessionID,
				request.AccountID,
				request.InstallID,
				request.UserID,
				deviceID(request.InstallID, request.AccountID),
			},
		},
		Attestation: attestation,
	}

	result, err := w.post(ctx, dump, oauth, request.OnDelta)
	result.Warnings = append(warnings, result.Warnings...)
	return result, dump, err
}

func (r Request) extraBetas(oauth bool) []string {
	if !oauth && r.CacheTTL == "1h" {
		return []string{ExtendedCacheTTLBeta}
	}
	return nil
}

func (w *Wire) endpoint(oauth bool) string {
	url := strings.TrimRight(w.config.BaseURL, "/") + MessagesPath
	if oauth {
		return url + OAuthQuery
	}
	return url
}

func (w *Wire) post(ctx context.Context, dump Dump, oauth bool, onDelta func(string)) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, w.config.Watchdog)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, dump.Method, dump.URL, strings.NewReader(string(dump.Body)))
	if err != nil {
		return Result{}, transport.Fail("anthropic.Ask", transport.KindBadRequest, err, "building the request")
	}
	for _, header := range dump.Headers {
		request.Header[header.Name] = []string{header.Value}
	}

	response, err := w.http.Do(request)
	if err != nil {
		return Result{}, transport.Fail("anthropic.Ask", transport.KindProvider, err, "posting the request")
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
		return Result{}, &transport.Error{
			Kind:   transport.StatusKind(response.StatusCode),
			Op:     "anthropic.Ask",
			Status: response.StatusCode,
			Detail: strings.TrimSpace(string(detail)),
		}
	}

	reader, err := decoded(response)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = reader.Close() }()
	return ReadStream(reader, oauth, onDelta)
}

func decoded(response *http.Response) (io.ReadCloser, error) {
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return response.Body, nil
	case "gzip":
		reader, err := gzip.NewReader(response.Body)
		if err != nil {
			return nil, transport.Fail("anthropic.Ask", transport.KindProvider, err, "opening the gzip stream")
		}
		return reader, nil
	case "deflate":
		return flate.NewReader(response.Body), nil
	}
	return nil, transport.Fail("anthropic.Ask", transport.KindProvider, nil,
		"the response is encoded as %q and this wire decodes only gzip and deflate",
		response.Header.Get("Content-Encoding"))
}
