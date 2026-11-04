package boardy

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	platformfs "tofu/internal/platform/fs"
)

const (
	requestsName    = "requests"
	requestIDPrefix = "R"
)

type RequestState string

const (
	RequestOpen       RequestState = "open"
	RequestUnreviewed RequestState = "unreviewed"
	RequestRejected   RequestState = "rejected"
	RequestDone       RequestState = "done"
)

type ReviewVerdict string

const (
	ReviewPass ReviewVerdict = "pass"
	ReviewFail ReviewVerdict = "fail"
)

func ParseReviewVerdict(raw string) (ReviewVerdict, error) {
	if verdict := ReviewVerdict(raw); verdict == ReviewPass || verdict == ReviewFail {
		return verdict, nil
	}
	return "", fmt.Errorf("%q is not a review verdict: pass or fail", raw)
}

type RequestReview struct {
	Verdict ReviewVerdict `json:"verdict"`
	Note    string        `json:"note"`
	By      string        `json:"by"`
	At      time.Time     `json:"at"`
	Seen    int           `json:"seen"`
}

type PersonRequest struct {
	ID      string         `json:"id"`
	Words   string         `json:"words"`
	Tickets []string       `json:"tickets"`
	By      string         `json:"by"`
	At      time.Time      `json:"at"`
	Review  *RequestReview `json:"review,omitempty"`
}

type RequestStanding struct {
	Request PersonRequest `json:"request"`
	Tickets []Front       `json:"tickets"`
	State   RequestState  `json:"state"`
	Why     string        `json:"why"`
}

func (s Store) requestFile(key, id string) (string, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(id, requestIDPrefix))
	if err != nil || n < 1 || requestIDPrefix+strconv.Itoa(n) != id {
		return "", fmt.Errorf("%q is not a request id, such as R1", id)
	}
	return filepath.Join(s.BoardDir(key), requestsName, id+".json"), nil
}

func (s Store) readPersonRequest(key, id string) (PersonRequest, error) {
	var r PersonRequest
	path, err := s.requestFile(key, id)
	if err != nil {
		return r, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("%s has no request %s: tofu boardy request list", key, id)
	}
	if err == nil {
		err = json.Unmarshal(raw, &r)
	}
	if err != nil {
		return r, fmt.Errorf("request %s of %s: %w", id, key, err)
	}
	return r, nil
}

func (s Store) savePersonRequest(key string, r PersonRequest) error {
	path, err := s.requestFile(key, r.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'))
}

func (s Store) requestIDs(key string) ([]string, error) {
	names, err := platformfs.ListFiles(filepath.Join(s.BoardDir(key), requestsName), ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var ids []string
	for _, name := range names {
		if id := strings.TrimSuffix(name, ".json"); !strings.HasPrefix(name, ".") {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int { return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b)) })
	return ids, err
}

func (m Managed) cutFrom(key string, held, ids []string) ([]string, error) {
	for _, id := range ids {
		ref, err := SplitID(id)
		if err == nil && ref.Key != key {
			err = fmt.Errorf("%s is on %s, and a request on %s holds only its own tickets", id, ref.Key, key)
		}
		if err == nil {
			_, err = m.Store.Get(id)
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(held, id) {
			held = append(held, id)
		}
	}
	return held, nil
}

func (m Managed) NewRequest(key, words string, ids []string, actor string) (RequestStanding, error) {
	if strings.TrimSpace(words) == "" {
		return RequestStanding{}, errors.New("a request holds the person's words, quoted as they wrote them")
	}
	held, err := m.cutFrom(key, nil, ids)
	if err != nil {
		return RequestStanding{}, err
	}
	r := PersonRequest{Words: words, Tickets: held, By: actor, At: time.Now().UTC().Truncate(time.Second)}
	err = m.Store.locked(key, requestsName, func() error {
		existing, err := m.Store.requestIDs(key)
		last := 0
		if len(existing) > 0 {
			last, _ = strconv.Atoi(strings.TrimPrefix(existing[len(existing)-1], requestIDPrefix))
		}
		r.ID = requestIDPrefix + strconv.Itoa(last+1)
		return errors.Join(err, m.Store.savePersonRequest(key, r))
	})
	if err != nil {
		return RequestStanding{}, err
	}
	return m.Store.standing(key, r)
}

func (m Managed) changeRequest(key, id string, change func(*PersonRequest) error) (RequestStanding, error) {
	var r PersonRequest
	err := m.Store.locked(key, requestsName, func() error {
		var err error
		if r, err = m.Store.readPersonRequest(key, id); err != nil {
			return err
		}
		if err := change(&r); err != nil {
			return err
		}
		return m.Store.savePersonRequest(key, r)
	})
	if err != nil {
		return RequestStanding{}, err
	}
	return m.Store.standing(key, r)
}

func (m Managed) AddToRequest(key, id string, ids []string, actor string) (RequestStanding, error) {
	return m.changeRequest(key, id, func(r *PersonRequest) (err error) {
		if r.Tickets, err = m.cutFrom(key, r.Tickets, ids); err == nil {
			r.Review = nil
		}
		return err
	})
}

func (m Managed) ReviewRequest(key, id string, verdict ReviewVerdict, note, actor string) (RequestStanding, error) {
	note = strings.Join(strings.Fields(note), " ")
	if note == "" {
		return RequestStanding{}, errors.New("a review says how the work holds against the person's words: --note text")
	}
	board, err := m.Store.Board(key)
	if err != nil {
		return RequestStanding{}, err
	}
	if m.role(board, actor) == RoleAgent {
		return RequestStanding{}, RefusedError{Why: actor + " is an agent on " + key + ", and only the person or a manager reviews a request against the person's words"}
	}
	return m.changeRequest(key, id, func(r *PersonRequest) error {
		now, err := m.Store.standing(key, *r)
		if err != nil {
			return err
		}
		if now.State == RequestOpen {
			return RefusedError{Why: r.ID + " is open, and a review reads finished work: " + now.Why}
		}
		events, err := m.Store.Events(key, 0)
		r.Review = &RequestReview{Verdict: verdict, Note: note, By: actor, At: time.Now().UTC().Truncate(time.Second), Seen: len(events)}
		return err
	})
}

func (s Store) RequestStanding(key, id string) (RequestStanding, error) {
	r, err := s.readPersonRequest(key, id)
	if err != nil {
		return RequestStanding{}, err
	}
	return s.standing(key, r)
}

func (s Store) RequestStandings(key string) ([]RequestStanding, error) {
	ids, err := s.requestIDs(key)
	standings := make([]RequestStanding, 0, len(ids))
	for _, id := range ids {
		standing, standingErr := s.RequestStanding(key, id)
		err = errors.Join(err, standingErr)
		standings = append(standings, standing)
	}
	return standings, err
}

func (s Store) standing(key string, r PersonRequest) (RequestStanding, error) {
	standing := RequestStanding{Request: r, Tickets: []Front{}, State: RequestOpen}
	var open []string
	var err error
	for _, id := range r.Tickets {
		ticket, getErr := s.Get(id)
		if getErr != nil {
			err = errors.Join(err, getErr)
			open = append(open, id+" unreadable")
			continue
		}
		standing.Tickets = append(standing.Tickets, ticket.Front)
		if ticket.Status != Done {
			open = append(open, id+" "+string(ticket.Status))
		}
	}
	review := r.Review
	switch {
	case len(r.Tickets) == 0:
		standing.Why = "no ticket is cut from it yet: tofu boardy request add " + r.ID + " <ticket>"
		return standing, err
	case len(open) > 0:
		standing.Why = "not done: " + strings.Join(open, ", ")
		return standing, err
	case review == nil:
		standing.State, standing.Why = RequestUnreviewed, "every ticket is done; review it against the words: tofu boardy request review "+r.ID+" pass|fail --note text"
		return standing, err
	}
	since, eventsErr := s.Events(key, review.Seen)
	if at := slices.IndexFunc(since, func(e Event) bool { return e.Kind == Moved && slices.Contains(r.Tickets, e.Ticket) }); at >= 0 {
		standing.State, standing.Why = RequestUnreviewed, since[at].Ticket+" moved after the review of "+review.At.Local().Format(time.DateTime)+"; review it again"
		return standing, errors.Join(err, eventsErr)
	}
	standing.State, standing.Why = RequestDone, "every ticket is done and the review by "+review.By+" passed: "+review.Note
	if review.Verdict == ReviewFail {
		standing.State, standing.Why = RequestRejected, "the review by "+review.By+" failed: "+review.Note
	}
	return standing, errors.Join(err, eventsErr)
}
