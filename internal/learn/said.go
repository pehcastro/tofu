package learn

import (
	"cmp"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"tofu/internal/session"
	"tofu/internal/sys"
	"tofu/internal/turn"
)

const (
	originTyped  = "typed by the person"
	originSteer  = "steer"
	originTask   = "task"
	originReport = "sub-agent report"
	environment  = "<env>"
	paragraph    = "\n\n"
	learnTask    = "tofu learn"
)

var ruleNoteAhead = regexp.MustCompile(`\A\[[^\]\n]+, from the rule [^\]\n]+\]\n(?:[^\n]+\n)*?\n`)

type Said struct {
	Session  string    `json:"session"`
	At       time.Time `json:"at"`
	Text     string    `json:"text"`
	Before   string    `json:"before"`
	After    string    `json:"after"`
	Answered string    `json:"answered,omitempty"`
	Place    string    `json:"place"`
	id       string
	store    *session.Store
}

type Source struct {
	Store   *session.Store
	Headers []session.Header
	Project string
}

type Read struct {
	Sessions []string  `json:"sessions"`
	Typed    int       `json:"typed"`
	Repeated int       `json:"repeated"`
	Calls    int       `json:"calls"`
	Reports  int       `json:"reports"`
	Forks    int       `json:"forks"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

type heard struct {
	Said
	taken bool
	seat  int
}

func Chain(store *session.Store, handle string) ([]session.Header, error) {
	named, err := store.Resolve(handle)
	if err != nil {
		return nil, err
	}
	last := named[0]
	chain, err := store.Ancestors(last.ID)
	if err != nil {
		return nil, err
	}
	slices.Reverse(chain)
	return append(chain, last), nil
}

func Recent(store *session.Store, count int) ([]session.Header, error) {
	listing, err := store.Listing()
	if err != nil {
		return nil, err
	}
	families := listing.Families()
	var recent []session.Header
	for _, family := range slices.Backward(families[:min(count, len(families))]) {
		recent = append(recent, family.Generations...)
	}
	return recent, nil
}

type spoken struct {
	at   time.Time
	body session.MessageBody
}

type sitting struct {
	id, name, place string
	store           *session.Store
	spoken          []spoken
	requests        []session.Exchange
}

func readSittings(sources []Source) ([]sitting, Read, error) {
	var read Read
	var sittings []sitting
	for _, source := range sources {
		for _, header := range source.Headers {
			if strings.Contains(header.Task, learnTask) {
				continue
			}
			one, err := readSitting(source.Store, header, &read)
			if err != nil {
				return nil, Read{}, err
			}
			one.place = cmp.Or(source.Project, one.name)
			sittings = append(sittings, one)
		}
	}
	return sittings, read, nil
}

func readSitting(store *session.Store, header session.Header, read *Read) (sitting, error) {
	events, err := store.Events(header.ID)
	if err != nil {
		return sitting{}, err
	}
	exchanges, err := store.Exchanges(header.ID)
	if err != nil {
		return sitting{}, err
	}
	one := sitting{id: header.ID, name: cmp.Or(header.Named(), header.ID), store: store, requests: slices.DeleteFunc(exchanges, func(e session.Exchange) bool { return e.Agent != "" })}
	for _, event := range events {
		var body session.MessageBody
		if event.Agent == "" && event.Kind == session.EventToolCall {
			read.Calls++
		}
		if event.Agent != "" || event.Kind != session.EventMessage || json.Unmarshal(event.Body, &body) != nil {
			continue
		}
		one.spoken = append(one.spoken, spoken{at: event.At, body: body})
		if body.Origin == originReport {
			read.Reports++
		}
	}
	read.Sessions = append(read.Sessions, one.name)
	if header.Parent != "" {
		read.Forks++
	}
	return one, nil
}

func Distil(sources []Source) ([]Said, Read, error) {
	sittings, read, err := readSittings(sources)
	if err != nil {
		return nil, Read{}, err
	}
	var wrapped []string
	for _, s := range sittings {
		for _, one := range s.spoken {
			if strings.HasPrefix(one.body.Content, environment) {
				wrapped = append(wrapped, one.body.Content)
			}
		}
	}
	slices.Sort(wrapped)
	wrapped = slices.Compact(wrapped)
	redact := sys.LoadKeyRedactor()
	var all []heard
	for seat, s := range sittings {
		for _, one := range s.spoken {
			if one.body.Role != session.RoleUser || !slices.Contains([]string{originTyped, originSteer, originTask}, one.body.Origin) {
				continue
			}
			found := heard{Said: Said{Session: s.name, Place: s.place, id: s.id, store: s.store, At: one.at, Text: redact.Redact(wordsOf(one.body.Content, wrapped))}, seat: seat}
			if one.body.TakenAt != nil {
				found.At, found.taken = *one.body.TakenAt, true
			}
			all = append(all, found)
		}
	}
	kept, repeated := firstOfEach(all)
	read.Repeated, read.Typed = repeated, len(kept)
	out := make([]Said, len(kept))
	for i, k := range kept {
		var until time.Time
		if next := slices.IndexFunc(kept[i+1:], func(other heard) bool { return other.seat == k.seat }); next >= 0 {
			until = kept[i+1+next].At
		}
		out[i] = windowOf(k.Said, sittings[k.seat], until)
		out[i].Before, out[i].After = redact.Redact(out[i].Before), redact.Redact(out[i].After)
	}
	if len(out) > 0 {
		read.From, read.To = out[0].At, out[len(out)-1].At
	}
	return out, read, nil
}

func wordsOf(raw string, wrapped []string) string {
	words := raw
	switch {
	case strings.Contains(raw, turn.TheTaskFollows):
		words, _ = turn.TaskIn(raw)
	case strings.HasPrefix(raw, environment):
		shared := 0
		for _, other := range slices.DeleteFunc(slices.Clone(wrapped), func(w string) bool { return w == raw }) {
			n := 0
			for n < len(raw) && n < len(other) && raw[n] == other[n] {
				n++
			}
			shared = max(shared, n)
		}
		if shared == 0 {
			shared = len(raw)
		}
		words = raw[strings.LastIndex(raw[:shared], paragraph)+len(paragraph):]
	}
	for {
		note := ruleNoteAhead.FindString(words)
		if note == "" {
			return strings.TrimSpace(words)
		}
		words = words[len(note):]
	}
}

func firstOfEach(all []heard) ([]heard, int) {
	times := map[string][]time.Time{}
	for _, one := range all {
		if one.taken && !slices.ContainsFunc(times[one.Text], one.At.Equal) {
			times[one.Text] = append(times[one.Text], one.At)
		}
	}
	var kept []heard
	seen := map[string]bool{}
	repeated := 0
	for _, one := range all {
		if one.Text == "" || seen[one.Text] {
			continue
		}
		seen[one.Text] = true
		if fires := len(times[one.Text]); fires > 1 {
			repeated += fires
			continue
		}
		kept = append(kept, one)
	}
	slices.SortStableFunc(kept, func(a, b heard) int { return a.At.Compare(b.At) })
	return kept, repeated
}

func windowOf(said Said, s sitting, until time.Time) Said {
	for _, one := range s.spoken {
		switch {
		case one.body.Role != session.RoleAssistant || strings.TrimSpace(one.body.Content) == "":
		case one.at.Before(said.At):
			said.Before = tail(one.body.Content)
		case said.After == "" && one.at.After(said.At) && (until.IsZero() || one.at.Before(until)):
			said.After = tail(one.body.Content)
		}
	}
	for _, request := range s.requests {
		if request.At.Before(said.At) {
			said.Answered = request.Request
		}
	}
	return said
}

func tail(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= TurnTailBytes {
		return text
	}
	cut := len(text) - TurnTailBytes
	for !utf8.RuneStart(text[cut]) {
		cut++
	}
	return text[cut:]
}
