package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	exchangesName = "requests.jsonl"
	blobsName     = "blobs.jsonl"
	blobHashBytes = 16
)

type BodyPart struct {
	Key   string   `json:"key"`
	Blob  string   `json:"blob,omitempty"`
	Items []string `json:"items,omitempty"`
}

type ExchangeAttempt struct {
	Body   []BodyPart      `json:"body,omitempty"`
	Detail json.RawMessage `json:"detail"`
}

type Exchange struct {
	Request    string            `json:"request"`
	Agent      string            `json:"agent,omitempty"`
	Turn       string            `json:"turn,omitempty"`
	At         time.Time         `json:"at"`
	Why        string            `json:"why,omitempty"`
	Wire       string            `json:"wire,omitempty"`
	Model      string            `json:"model,omitempty"`
	ToolChoice string            `json:"tool_choice,omitempty"`
	Messages   []string          `json:"messages"`
	Tools      string            `json:"tools,omitempty"`
	Attempts   []ExchangeAttempt `json:"attempts,omitempty"`
	Response   string            `json:"response,omitempty"`
	Error      string            `json:"error,omitempty"`
	DurationMS int64             `json:"duration_ms"`
}

type blobLine struct {
	Hash string          `json:"hash"`
	Body json.RawMessage `json:"body"`
}

func (l *Log) Keep(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.keep(raw)
}

func (l *Log) keep(raw []byte) (string, error) {
	text, masked := string(raw), raw
	if redacted := l.redact.Redact(text); redacted != text {
		masked = []byte(redacted)
	}
	hash := BlobHash(masked)
	if l.blobs == nil {
		kept, err := l.store.Blobs(l.header.ID)
		if err != nil {
			return "", err
		}
		l.blobs = map[string]bool{}
		for known := range kept {
			l.blobs[known] = true
		}
	}
	if l.blobs[hash] {
		return hash, nil
	}
	if err := l.appendLine(blobsName, slices.Concat([]byte(`{"hash":"`+hash+`","body":`), masked, []byte("}"))); err != nil {
		return "", err
	}
	l.blobs[hash] = true
	return hash, nil
}

func BlobHash(masked []byte) string {
	sum := sha256.Sum256(masked)
	return hex.EncodeToString(sum[:blobHashBytes])
}

func (l *Log) KeepBody(body []byte) ([]BodyPart, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if opening, err := decoder.Token(); err != nil || opening != json.Delim('{') {
		hash, keepErr := l.Keep(string(body))
		return []BodyPart{{Blob: hash}}, keepErr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var parts []BodyPart
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		part := BodyPart{Key: fmt.Sprint(key)}
		var items []json.RawMessage
		if bytes.HasPrefix(bytes.TrimSpace(value), []byte("[")) && json.Unmarshal(value, &items) == nil {
			part.Items = make([]string, 0, len(items))
			for _, item := range items {
				hash, err := l.keep(item)
				if err != nil {
					return nil, err
				}
				part.Items = append(part.Items, hash)
			}
		} else if part.Blob, err = l.keep(value); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func (l *Log) Exchanged(exchange Exchange) error {
	line, err := json.Marshal(exchange)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.appendLine(exchangesName, []byte(l.redact.Redact(string(line))))
}

func (l *Log) appendLine(name string, line []byte) error {
	file, open := l.side[name]
	if !open {
		var err error
		if file, err = os.OpenFile(filepath.Join(l.store.Dir(l.header.ID), name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			return err
		}
		l.side[name] = file
	}
	_, err := file.Write(append(line, '\n'))
	return err
}

func (l *Log) closeSide() error {
	var closed []error
	for name, file := range l.side {
		closed = append(closed, file.Close())
		delete(l.side, name)
	}
	return errors.Join(closed...)
}

func eachLine(path string, read func(line []byte) error) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for {
		line, rest, terminated := bytes.Cut(raw, []byte{'\n'})
		if !terminated {
			return nil
		}
		raw = rest
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if readErr := read(trimmed); readErr != nil {
				return fmt.Errorf("%s: %w", filepath.Base(path), readErr)
			}
		}
	}
}

type Sizes struct {
	Events   int64 `json:"events_bytes"`
	Requests int64 `json:"requests_bytes"`
	Blobs    int64 `json:"blobs_bytes"`
}

func (s *Store) Sizes(id string) Sizes {
	size := func(name string) int64 {
		info, err := os.Stat(filepath.Join(s.Dir(id), name))
		if err != nil {
			return 0
		}
		return info.Size()
	}
	return Sizes{Events: size(eventsName), Requests: size(exchangesName), Blobs: size(blobsName)}
}

type sentList struct {
	raw    json.RawMessage
	hashes []string
}

func (sent sentList) extendedBy(raw json.RawMessage) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	opened := sent.raw[:max(len(sent.raw)-1, 0)]
	if len(sent.hashes) > 0 && len(raw) > len(opened) && bytes.HasPrefix(raw, opened) && raw[len(opened)] == ',' {
		var added []string
		err := json.Unmarshal(slices.Concat([]byte("["), raw[len(opened)+1:]), &added)
		return append(sent.hashes, added...), err
	}
	var hashes []string
	err := json.Unmarshal(raw, &hashes)
	return hashes, err
}

func (s *Store) Exchanges(id string) ([]Exchange, error) {
	var exchanges []Exchange
	sent := map[string]sentList{}
	err := eachLine(filepath.Join(s.Dir(id), exchangesName), func(line []byte) error {
		var read struct {
			Exchange
			Messages json.RawMessage `json:"messages"`
		}
		if json.Unmarshal(line, &read) != nil {
			return json.Unmarshal(line, &Exchange{})
		}
		hashes, err := sent[read.Agent].extendedBy(read.Messages)
		if err != nil {
			return json.Unmarshal(line, &Exchange{})
		}
		sent[read.Agent] = sentList{raw: read.Messages, hashes: hashes}
		read.Exchange.Messages = hashes[:len(hashes):len(hashes)]
		exchanges = append(exchanges, read.Exchange)
		return nil
	})
	return exchanges, err
}

func (s *Store) Blobs(id string) (map[string]json.RawMessage, error) {
	blobs := map[string]json.RawMessage{}
	err := eachLine(filepath.Join(s.Dir(id), blobsName), func(line []byte) error {
		var blob blobLine
		if err := json.Unmarshal(line, &blob); err != nil {
			return err
		}
		blobs[blob.Hash] = blob.Body
		return nil
	})
	return blobs, err
}

func Assemble(parts []BodyPart, blobs map[string]json.RawMessage) json.RawMessage {
	if len(parts) == 1 && parts[0].Key == "" {
		return blobs[parts[0].Blob]
	}
	var body bytes.Buffer
	body.WriteByte('{')
	for i, part := range parts {
		if i > 0 {
			body.WriteByte(',')
		}
		key, _ := json.Marshal(part.Key)
		body.Write(key)
		body.WriteByte(':')
		if part.Items == nil {
			body.Write(blobs[part.Blob])
			continue
		}
		body.WriteByte('[')
		for j, item := range part.Items {
			if j > 0 {
				body.WriteByte(',')
			}
			body.Write(blobs[item])
		}
		body.WriteByte(']')
	}
	body.WriteByte('}')
	return body.Bytes()
}
