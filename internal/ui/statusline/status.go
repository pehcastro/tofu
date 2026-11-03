package status

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type State string

const (
	Idle    State = "idle"
	Working State = "working"
	Done    State = "done"
	Blocked State = "blocked"
	Errored State = "error"
	Clear   State = "clear"
)

type Kind string

const (
	Permission Kind = "permission"
	Question   Kind = "question"
	Auth       Kind = "auth"
)

type Record struct {
	ID       string    `json:"id,omitempty"`
	State    State     `json:"state"`
	Kind     Kind      `json:"kind,omitempty"`
	Progress *int      `json:"progress,omitempty"`
	App      string    `json:"app,omitempty"`
	Title    string    `json:"title,omitempty"`
	Msg      string    `json:"msg,omitempty"`
	Ask      string    `json:"ask,omitempty"`
	At       time.Time `json:"at,omitzero"`
}

func (State) Enum() []string {
	return []string{string(Idle), string(Working), string(Done), string(Blocked), string(Errored), string(Clear)}
}

func (s State) Finished() bool { return s == Done || s == Errored }

func (Kind) Enum() []string { return []string{string(Permission), string(Question), string(Auth)} }

const (
	introducer    = "\x1b]7501;"
	terminator    = "\x1b\\"
	segmentBytes  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.+-"
	valueBytes    = segmentBytes + ",/="
	maxSequence   = 4096
	maxKey        = 16
	maxMsg        = 2048
	maxMsgEncoded = 2732
	maxTitle      = 192
	maxTitleCoded = 256
	maxApp        = 32
	maxID         = 128
	maxSegment    = 32
	maxDepth      = 8
	maxRecords    = 256
	maxProgress   = 100
)

func Encode(r Record) string {
	pairs := []string{"state=" + string(r.State)}
	if r.ID != "" {
		pairs = append(pairs, "id="+r.ID)
	}
	if r.State == Clear {
		return introducer + strings.Join(pairs, ":") + terminator
	}
	if r.State == Blocked && r.Kind != "" {
		pairs = append(pairs, "kind="+string(r.Kind))
	}
	if (r.State == Working || r.State == Blocked) && r.Progress != nil {
		pairs = append(pairs, "progress="+strconv.Itoa(*r.Progress))
	}
	if r.App != "" {
		pairs = append(pairs, "app="+r.App)
	}
	if r.Title != "" {
		pairs = append(pairs, "title="+base64.StdEncoding.EncodeToString([]byte(printable(r.Title, maxTitle))))
	}
	if r.Msg != "" {
		pairs = append(pairs, "msg="+base64.StdEncoding.EncodeToString([]byte(printable(r.Msg, maxMsg))))
	}
	return introducer + strings.Join(pairs, ":") + terminator
}

func printable(text string, limit int) string {
	text = strings.Map(func(r rune) rune {
		if control(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(text, " "))
	if len(text) <= limit {
		return text
	}
	cut := limit
	for !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func control(r rune) bool {
	return r < 0x20 || r >= 0x7f && r <= 0x9f
}

func Path(parts ...string) string {
	var id string
	for index, part := range parts {
		segment := segmentOf(part)
		joined := id + "/" + segment
		if index == 0 {
			joined = segment
		}
		if index == maxDepth || len(joined) > maxID {
			break
		}
		id = joined
	}
	return id
}

func segmentOf(name string) string {
	if name == "" {
		return "_"
	}
	segment := []byte(name[:min(len(name), maxSegment)])
	for index, b := range segment {
		if strings.IndexByte(segmentBytes, b) < 0 {
			segment[index] = '_'
		}
	}
	return string(segment)
}

func same(a, b Record) bool {
	progressA, progressB := -1, -1
	if a.Progress != nil {
		progressA = *a.Progress
	}
	if b.Progress != nil {
		progressB = *b.Progress
	}
	a.Progress, b.Progress = nil, nil
	return a == b && progressA == progressB
}
