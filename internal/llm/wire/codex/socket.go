package codex

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/transport"
)

var errNoSocket = errors.New("the socket is busy, failed or not offered, so the request goes over http")

type socket struct {
	url        string
	busy       sync.Mutex
	conn       *websocket.Conn
	failed     bool
	properties map[string]json.RawMessage
	input      []json.RawMessage
	response   string
}

type socketFailure struct {
	Type   string `json:"type"`
	Status int    `json:"status"`
	Code   int    `json:"status_code"`
	Error  struct {
		Code string `json:"code"`
	} `json:"error"`
	Header map[string]string `json:"headers"`
}

type socketLimits struct {
	Type       string `json:"type"`
	RateLimits map[string]*struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes *int64  `json:"window_minutes"`
		ResetAt       *int64  `json:"reset_at"`
	} `json:"rate_limits"`
}

type sentItem struct {
	Type string `json:"type"`
	Role string `json:"role"`
}

func (w *Wire) overSocket(ctx context.Context, dump Dump, onThinking func(string)) (Result, error) {
	if dump.URL != SubscriptionBaseURL+SubscriptionPath || !w.socket.busy.TryLock() {
		return Result{}, errNoSocket
	}
	defer w.socket.busy.Unlock()
	if w.socket.failed {
		return Result{}, errNoSocket
	}
	note := llm.Record(ctx, llm.Attempt{At: time.Now(), URL: w.socket.url, Status: http.StatusSwitchingProtocols})
	properties, input, err := w.sendFrame(ctx, dump, note)
	if err != nil {
		return Result{}, err
	}

	conn := w.socket.conn
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()
	conn.SetReadLimit(konst.StreamLineBytes)
	heard := false
	result, err := readEvents(func() ([]byte, error) {
		_ = conn.SetReadDeadline(time.Now().Add(w.config.StreamIdle))
		_, payload, err := conn.ReadMessage()
		note.Received(len(payload), err)
		if err != nil {
			return nil, transport.Fail("codex.Ask", transport.KindProvider, errors.Join(llm.ErrStreamBroke, cmp.Or(ctx.Err(), err)), "reading the socket")
		}
		if !heard {
			heard = true
			note.Change(func(attempt *llm.Attempt) { attempt.HeadersMS = time.Since(attempt.At).Milliseconds() })
		}
		return payload, w.socketEvent(payload, note)
	}, onThinking)
	if err == nil {
		w.socket.properties, w.socket.input, w.socket.response = properties, input, result.ID
		return result, nil
	}
	w.dropSocket()
	if heard || ctx.Err() != nil {
		return result, err
	}
	w.socket.failed = true
	return Result{}, errNoSocket
}

func (w *Wire) sendFrame(ctx context.Context, dump Dump, note llm.Note) (map[string]json.RawMessage, []json.RawMessage, error) {
	for {
		fresh := w.socket.conn == nil
		if fresh {
			if err := w.dial(ctx, dump, note); err != nil {
				return nil, nil, err
			}
		}
		frame, properties, input, err := w.socket.frame(dump)
		if err != nil {
			return nil, nil, err
		}
		note.Change(func(attempt *llm.Attempt) { attempt.Body = frame })
		_ = w.socket.conn.SetWriteDeadline(time.Now().Add(w.config.StreamIdle))
		err = w.socket.conn.WriteMessage(websocket.TextMessage, frame)
		if err == nil {
			return properties, input, nil
		}
		w.dropSocket()
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if fresh {
			w.socket.failed = true
			note.Change(func(attempt *llm.Attempt) { attempt.Error = "sending over the socket: " + err.Error() })
			return nil, nil, errNoSocket
		}
	}
}

func (w *Wire) dial(ctx context.Context, dump Dump, note llm.Note) error {
	header := http.Header{}
	for _, sent := range dump.Headers {
		switch sent.Name {
		case HeaderAccept, HeaderContentType, HeaderTurnState:
		case HeaderBeta:
			header.Set(sent.Name, BetaResponsesSocket)
		default:
			header[sent.Name] = []string{sent.Value}
		}
	}
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: w.config.StreamIdle, EnableCompression: true}
	conn, response, err := dialer.DialContext(ctx, w.socket.url, header)
	note.Change(func(attempt *llm.Attempt) {
		attempt.Status = 0
		if response != nil {
			attempt.Status = response.StatusCode
			attempt.RequestID = cmp.Or(response.Header.Get("request-id"), response.Header.Get("x-request-id"))
			attempt.RetryAfter = response.Header.Get("retry-after")
		}
	})
	if response != nil && w.config.OnHeaders != nil {
		w.config.OnHeaders(response.Header)
	}
	if err == nil {
		w.socket.conn = conn
		return nil
	}
	note.Change(func(attempt *llm.Attempt) { attempt.Error = "opening the socket: " + err.Error() })
	if response != nil && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusTooManyRequests) {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, konst.TransportErrorDetailBytes))
		return Refusal(response.StatusCode, response.Header, detail)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	w.socket.failed = true
	return errNoSocket
}

func (s *socket) frame(dump Dump) (frame []byte, properties map[string]json.RawMessage, input []json.RawMessage, err error) {
	var body map[string]json.RawMessage
	metadata := map[string]string{}
	if err := json.Unmarshal(dump.Body, &body); err != nil {
		return nil, nil, nil, transport.Fail("codex.Ask", transport.KindBadRequest, err, "reading the body back for the socket")
	}
	if err := json.Unmarshal(body["input"], &input); err != nil {
		return nil, nil, nil, transport.Fail("codex.Ask", transport.KindBadRequest, err, "reading the input back for the socket")
	}
	if raw, ok := body["client_metadata"]; ok {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, nil, nil, transport.Fail("codex.Ask", transport.KindBadRequest, err, "reading the client metadata back for the socket")
		}
	}
	for _, header := range dump.Headers {
		if header.Name == HeaderTurnState {
			metadata[HeaderTurnState] = header.Value
		}
	}
	properties = maps.Clone(body)
	delete(properties, "input")
	delete(properties, "client_metadata")
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, nil, nil, transport.Fail("codex.Ask", transport.KindBadRequest, err, "encoding the client metadata for the socket")
	}
	body["type"], body["client_metadata"] = json.RawMessage(`"response.create"`), encoded
	delete(body, "stream")
	if added := s.added(properties, input); added != nil {
		previous, _ := json.Marshal(s.response)
		body["input"], body["previous_response_id"] = added, previous
	}
	frame, err = json.Marshal(body)
	return frame, properties, input, err
}

func (s *socket) added(properties map[string]json.RawMessage, input []json.RawMessage) json.RawMessage {
	same := func(a, b json.RawMessage) bool { return bytes.Equal(a, b) }
	if s.response == "" || len(input) <= len(s.input) || !maps.EqualFunc(properties, s.properties, same) ||
		!slices.EqualFunc(input[:len(s.input)], s.input, same) {
		return nil
	}
	from := len(s.input)
	for ; from < len(input); from++ {
		var item sentItem
		_ = json.Unmarshal(input[from], &item)
		if item.Type != "reasoning" && item.Type != "function_call" && (item.Type != "message" || item.Role != "assistant") {
			break
		}
	}
	if from == len(input) {
		return nil
	}
	added, err := json.Marshal(input[from:])
	if err != nil {
		return nil
	}
	return added
}

func (w *Wire) socketEvent(payload []byte, note llm.Note) error {
	var failure socketFailure
	if json.Unmarshal(payload, &failure) == nil && failure.Type == "error" {
		if failure.Error.Code == "previous_response_not_found" || failure.Error.Code == "websocket_connection_limit_reached" {
			return transport.Fail("codex.Ask", transport.KindProvider, llm.ErrStreamBroke, "the socket asked for a fresh start: %s", failure.Error.Code)
		}
		if status := cmp.Or(failure.Status, failure.Code); status >= http.StatusBadRequest {
			note.Change(func(attempt *llm.Attempt) {
				attempt.Status, attempt.ErrorBody, attempt.RetryAfter = status, string(payload), failure.Header["retry-after"]
			})
			return Refusal(status, http.Header{transport.RetryAfterHeader: {failure.Header["retry-after"]}}, payload)
		}
	}
	var limits socketLimits
	if w.config.OnHeaders == nil || json.Unmarshal(payload, &limits) != nil || limits.Type != "codex.rate_limits" {
		return nil
	}
	header := http.Header{}
	for name, window := range limits.RateLimits {
		if window == nil {
			continue
		}
		prefix := "x-codex-" + name + "-"
		header.Set(prefix+"used-percent", strconv.FormatFloat(window.UsedPercent, 'f', -1, 64))
		if window.WindowMinutes != nil {
			header.Set(prefix+"window-minutes", strconv.FormatInt(*window.WindowMinutes, 10))
		}
		if window.ResetAt != nil {
			header.Set(prefix+"reset-at", strconv.FormatInt(*window.ResetAt, 10))
		}
	}
	w.config.OnHeaders(header)
	return nil
}

func (w *Wire) dropSocket() {
	if w.socket.conn != nil {
		_ = w.socket.conn.Close()
	}
	w.socket.conn, w.socket.properties, w.socket.input, w.socket.response = nil, nil, nil, ""
}

func socketURL() string {
	return cmp.Or(os.Getenv(SocketURLVariable), SubscriptionSocketURL)
}
