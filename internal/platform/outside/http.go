package outside

import (
	"cmp"
	"io"
	"net/http"
)

type clockedTrip struct {
	base http.RoundTripper
	kind Kind
}

type clockedBody struct {
	io.ReadCloser
	stop func()
}

func Client(kind Kind, given *http.Client) *http.Client {
	var client http.Client
	if given != nil {
		client = *given
	}
	client.Transport = clockedTrip{base: cmp.Or(client.Transport, http.DefaultTransport), kind: kind}
	return &client
}

func (c clockedTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	stop := Wait(request.Context(), c.kind)
	response, err := c.base.RoundTrip(request)
	if err != nil || response.Body == nil {
		stop()
		return response, err
	}
	response.Body = clockedBody{ReadCloser: response.Body, stop: stop}
	return response, nil
}

func (b clockedBody) Read(into []byte) (int, error) {
	read, err := b.ReadCloser.Read(into)
	if err != nil {
		b.stop()
	}
	return read, err
}

func (b clockedBody) Close() error {
	b.stop()
	return b.ReadCloser.Close()
}
