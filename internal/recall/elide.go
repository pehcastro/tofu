package recall

type Reference struct {
	ID    string
	Bytes int
	Head  string
	Tail  string
}

type Elided struct {
	Body      []byte
	Reference *Reference
}

func Elide(store *Store, cfg Config, body []byte, enabled bool) (Elided, error) {
	if len(body) <= cfg.ElideAboveBytes {
		return Elided{Body: body}, nil
	}
	id, err := store.put(body)
	if err != nil {
		return Elided{}, err
	}
	if !enabled {
		return Elided{Body: body}, nil
	}
	return Elided{Reference: &Reference{
		ID:    id,
		Bytes: len(body),
		Head:  string(clipHead(body, cfg.HeadBytes)),
		Tail:  string(clipTail(body, cfg.TailBytes)),
	}}, nil
}

func clipHead(body []byte, n int) []byte {
	if n >= len(body) {
		return body
	}
	return body[:n]
}

func clipTail(body []byte, n int) []byte {
	if n >= len(body) {
		return body
	}
	return body[len(body)-n:]
}
