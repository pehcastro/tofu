package codex

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

const Name = llm.WireCodex

type Config struct {
	BaseURL        string
	Model          string
	Token          llm.TokenSource
	HTTP           *http.Client
	Transport      transport.Config
	Account        transport.Account
	StreamIdle     time.Duration
	InstallationID string
	SessionID      string
	ClientVersion  string
	OnHeaders      func(http.Header)
}

type Wire struct {
	config   Config
	http     *http.Client
	threadID string
	windowID string
	routing  sync.Mutex
	routes   map[string]turnRoute
	socket   socket
}

type turnRoute struct {
	id    string
	state string
}

func New(config Config) (*Wire, error) {
	if config.Model == "" {
		return nil, transport.Fail("codex.New", transport.KindBadRequest, nil, "the wire has no model")
	}
	if config.Token == nil {
		return nil, transport.Fail("codex.New", transport.KindMissingCredential, nil, "the wire has no token source")
	}
	if config.StreamIdle <= 0 {
		config.StreamIdle = konst.StreamIdleMillis * time.Millisecond
	}
	threadID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	windowID, err := randomUUID()
	if err != nil {
		return nil, err
	}
	return &Wire{config: config, http: llm.StreamingClient(config.HTTP, config.Transport, config.StreamIdle),
		threadID: threadID, windowID: windowID, routes: map[string]turnRoute{}, socket: socket{url: socketURL()}}, nil
}

func (w *Wire) route(turn string) (turnRoute, error) {
	if turn == "" {
		return turnRoute{}, nil
	}
	w.routing.Lock()
	defer w.routing.Unlock()
	if known, ok := w.routes[turn]; ok {
		return known, nil
	}
	id, err := randomUUID()
	if err != nil {
		return turnRoute{}, err
	}
	w.routes[turn] = turnRoute{id: id}
	return w.routes[turn], nil
}

func (w *Wire) keepTurnState(turn, state string) {
	if turn == "" || state == "" {
		return
	}
	w.routing.Lock()
	defer w.routing.Unlock()
	if known := w.routes[turn]; known.state == "" {
		w.routes[turn] = turnRoute{id: known.id, state: state}
	}
}

type Dump struct {
	llm.Dump
	Subscription bool
	Plan         string
}

func (w *Wire) Ask(ctx context.Context, request Request) (Result, Dump, error) {
	request.Model = cmp.Or(request.Model, w.config.Model)
	request.Identity.InstallationID = cmp.Or(request.Identity.InstallationID, w.config.InstallationID)
	request.Identity.SessionID = cmp.Or(request.Identity.SessionID, w.config.SessionID)
	request.Identity.ThreadID = cmp.Or(request.Identity.ThreadID, w.threadID)
	request.Identity.WindowID = cmp.Or(request.Identity.WindowID, w.windowID)
	route, err := w.route(request.Turn)
	if err != nil {
		return Result{}, Dump{}, err
	}
	request.Identity.TurnID = cmp.Or(request.Identity.TurnID, route.id)
	request.TurnState = cmp.Or(request.TurnState, route.state)
	identity, err := request.Identity.filled()
	if err != nil {
		return Result{}, Dump{}, err
	}
	request.Identity = identity

	token, err := w.config.Token(ctx, "")
	if err != nil {
		return Result{}, Dump{}, err
	}
	if token == "" {
		return Result{}, Dump{}, transport.Fail("codex.Ask", transport.KindMissingCredential, nil,
			"the token source returned nothing")
	}
	result, dump, err := w.send(ctx, request, token)
	renewed, err := llm.Renewed(ctx, err, token, w.config.Token)
	if renewed != "" {
		result, dump, err = w.send(ctx, request, renewed)
	}
	w.keepTurnState(request.Turn, result.TurnState)
	if refused := request.RefusedControls(); len(refused) > 0 {
		result.Warnings = append([]string{
			"the codex backend refuses these and they were dropped: " + strings.Join(refused, ", "),
		}, result.Warnings...)
	}
	return result, dump, err
}

func (w *Wire) send(ctx context.Context, request Request, token string) (Result, Dump, error) {
	claims := ReadClaims(token)
	subscription := claims.Subscription()
	identity := request.Identity
	metadataHeader, clientMetadata, err := identity.TurnMetadata()
	if err != nil {
		return Result{}, Dump{}, err
	}
	if !subscription {
		clientMetadata = nil
	}
	body, err := request.Encode(clientMetadata)
	if err != nil {
		return Result{}, Dump{}, err
	}

	dump := Dump{
		Subscription: subscription,
		Plan:         claims.PlanType,
		Dump: llm.Dump{
			Method: http.MethodPost,
			URL:    w.endpoint(subscription),
			Headers: Headers(HeaderOptions{
				Token:        token,
				Subscription: subscription,
				Claims:       claims,
				Model:        request.Model,
				ServiceTier:  request.ServiceTier,
				Identity:     identity,
				TurnMetadata: metadataHeader,
				TurnState:    request.TurnState,
				Version:      w.config.ClientVersion,
			}),
			Body: body,
			Identifiers: []string{
				identity.InstallationID,
				identity.SessionID,
				identity.ThreadID,
				identity.WindowID,
				identity.TurnID,
				claims.AccountID,
			},
		},
	}

	sent := time.Now()
	result, err := llm.RetryQuiet(ctx, w.config.Transport, request.OnRetry, func() (Result, error) {
		if result, err := w.overSocket(ctx, dump, request.OnThinking); !errors.Is(err, errNoSocket) {
			return result, err
		}
		return w.post(ctx, dump, request.OnThinking)
	})
	result.FirstTokenMS = llm.MillisSince(sent, result.firstDelta)
	var refused *transport.Error
	if errors.As(err, &refused) {
		refused.Account = w.config.Account
		refused.Account.Plan = cmp.Or(refused.Account.Plan, claims.PlanType)
	}
	return result, dump, err
}

type refusalBody struct {
	Error struct {
		Code     string `json:"code"`
		Type     string `json:"type"`
		Message  string `json:"message"`
		ResetsAt int64  `json:"resets_at"`
	} `json:"error"`
	Detail string `json:"detail"`
}

func Refusal(status int, header http.Header, body []byte) *transport.Error {
	var said refusalBody
	_ = json.Unmarshal(body, &said)
	refused := &transport.Error{Kind: transport.StatusKind(status), Op: "codex.Ask", Status: status, Detail: strings.TrimSpace(string(body)),
		Code: cmp.Or(said.Error.Code, said.Error.Type), Said: cmp.Or(said.Error.Message, said.Detail), ResetAt: transport.ResetAt(header, time.Now())}
	if said.Error.ResetsAt > 0 {
		refused.ResetAt = time.Unix(said.Error.ResetsAt, 0)
	}
	return refused
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

func (w *Wire) post(ctx context.Context, dump Dump, onThinking func(string)) (Result, error) {
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
	if w.config.OnHeaders != nil {
		w.config.OnHeaders(response.Header)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
		return Result{}, Refusal(response.StatusCode, response.Header, detail)
	}

	body := llm.WatchIdle(response.Body, w.config.StreamIdle)
	defer func() { _ = body.Close() }()
	result, err := ReadStream(body, onThinking)
	if turnState := response.Header.Get(HeaderTurnState); turnState != "" {
		result.TurnState = turnState
	}
	return result, err
}
