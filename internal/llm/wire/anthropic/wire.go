package anthropic

import (
	"bytes"
	"cmp"
	"compress/flate"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
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
	BaseURL    string
	Model      string
	Token      TokenSource
	HTTP       *http.Client
	Transport  transport.Config
	Watchdog   time.Duration
	StreamIdle time.Duration
	Proxy      bool
	SessionID  string
	AccountID  string
	InstallID  string

	ClaudeCodeVersion string
	AdoptVersion      func(version string) error
}

type Wire struct {
	config  Config
	http    *http.Client
	mu      sync.Mutex
	version string
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
	if config.StreamIdle <= 0 {
		config.StreamIdle = konst.StreamIdleMillis * time.Millisecond
	}
	client := &http.Client{}
	if config.HTTP != nil {
		*client = *config.HTTP
	}
	client.Transport = llm.Retrying(client.Transport, config.Transport)
	return &Wire{config: config, http: client, version: cmp.Or(config.ClaudeCodeVersion, PinnedClaudeCodeVersion)}, nil
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

	w.mu.Lock()
	request.ClaudeCodeVersion = w.version
	w.mu.Unlock()
	result, dump, err := w.send(ctx, request, token, oauth)
	required := requiredVersion(err)
	if NewerVersion(request.ClaudeCodeVersion, required) == request.ClaudeCodeVersion {
		return result, dump, err
	}

	w.mu.Lock()
	w.version = NewerVersion(w.version, required)
	w.mu.Unlock()
	var unsaved error
	if w.config.AdoptVersion != nil {
		unsaved = w.config.AdoptVersion(required)
	}
	request.ClaudeCodeVersion = required
	result, dump, err = w.send(ctx, request, token, oauth)
	if unsaved != nil {
		result.Warnings = append(result.Warnings, "claude code "+required+" is claimed for this session and was not saved: "+unsaved.Error())
	}
	return result, dump, err
}

func requiredVersion(err error) string {
	var failure *transport.Error
	if !errors.As(err, &failure) || !strings.Contains(failure.Detail, VersionTooOldCode) {
		return ""
	}
	required := regexp.MustCompile(`version (\d+\.\d+\.\d+) or newer is required`).FindStringSubmatch(failure.Detail)
	if required == nil {
		return ""
	}
	return required[1]
}

func (w *Wire) send(ctx context.Context, request Request, token string, oauth bool) (Result, Dump, error) {
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
				AgentRequest: len(request.Tools) > 0 || request.Effort.Thinks(),
				Thinking:     request.Effort.Thinks(),
				SessionID:    request.SessionID,
				ExtraBetas:   request.extraBetas(oauth),
				Version:      request.ClaudeCodeVersion,
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

	sent := time.Now()
	result, err := llm.RetryQuiet(ctx, w.config.Transport, request.OnRetry, func() (Result, error) {
		return w.post(ctx, dump, oauth, request.OnDelta, request.OnThinking)
	})
	result.FirstTokenMS = llm.MillisSince(sent, result.firstDelta)
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

func (w *Wire) post(ctx context.Context, dump Dump, oauth bool, onDelta, onThinking func(string)) (Result, error) {
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
		return Result{}, &transport.Error{
			Kind:   transport.StatusKind(response.StatusCode),
			Op:     "anthropic.Ask",
			Status: response.StatusCode,
			Detail: errorDetail(response),
		}
	}

	body := llm.WatchIdle(response.Body, w.config.StreamIdle)
	defer func() { _ = body.Close() }()
	reader, err := decoded(normalizedEncoding(response.Header.Get("Content-Encoding")), body)
	if err != nil {
		return Result{}, transport.Fail("anthropic.Ask", transport.KindProvider, err, "decoding the response")
	}
	defer func() { _ = reader.Close() }()
	return ReadStream(reader, oauth, onDelta, onThinking)
}

func normalizedEncoding(header string) string {
	return strings.ToLower(strings.TrimSpace(header))
}

func errorDetail(response *http.Response) string {
	encoding := normalizedEncoding(response.Header.Get("Content-Encoding"))
	raw, _ := io.ReadAll(io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
	reader, err := decoded(encoding, io.NopCloser(bytes.NewReader(raw)))
	var text []byte
	if err == nil {
		text, err = io.ReadAll(reader)
	}
	if err != nil {
		return fmt.Sprintf("the body is encoded as %q, %d bytes read, and could not be decoded: %v", encoding, len(raw), err)
	}
	return strings.TrimSpace(string(text))
}

func decoded(encoding string, body io.ReadCloser) (io.ReadCloser, error) {
	switch encoding {
	case "", "identity":
		return body, nil
	case "gzip":
		reader, err := gzip.NewReader(body)
		if err != nil {
			return nil, fmt.Errorf("opening the gzip stream: %w", err)
		}
		return reader, nil
	case "deflate":
		return flate.NewReader(body), nil
	}
	return nil, fmt.Errorf("this wire decodes only gzip and deflate, not %q", encoding)
}
