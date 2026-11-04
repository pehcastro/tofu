package host

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tofu/internal/agent/loop"
	"tofu/internal/store/session"
)

type standingIn struct {
	store *session.Store
	id    string
}

func (h *Host) standingIn() standingIn {
	store, err := session.OpenIn(h.dir)
	if err != nil {
		return standingIn{}
	}
	return standingIn{store: store, id: h.ID()}
}

func (h *Host) standingUnkept(err error) {
	h.note("the standing answer holds only until tofu restarts, it was not written beside the session: " + err.Error())
}

func (in standingIn) file() string {
	return filepath.Join(in.store.Dir(in.id), "standing.json")
}

func (in standingIn) forkedInto(id string) bool {
	if in.id == "" {
		return false
	}
	header, err := in.store.Header(in.id)
	return err == nil && header.ForkedInto == id
}

func (in standingIn) unwrittenForkOf(before standingIn) bool {
	if in.id == "" {
		return false
	}
	_, err := os.Stat(in.file())
	return errors.Is(err, fs.ErrNotExist) && before.forkedInto(in.id)
}

func (in standingIn) carried() ([]byte, error) {
	body, err := os.ReadFile(in.file())
	if !errors.Is(err, fs.ErrNotExist) {
		return body, err
	}
	chain, chainErr := in.store.Ancestors(in.id)
	if chainErr != nil {
		return nil, err
	}
	into := in.id
	for _, from := range chain {
		if from.ForkedInto != into {
			break
		}
		if body, err = os.ReadFile(standingIn{store: in.store, id: from.ID}.file()); !errors.Is(err, fs.ErrNotExist) {
			return body, err
		}
		into = from.ID
	}
	return nil, err
}

func (in standingIn) read() (standing map[string]Answer, until, given map[string]time.Time, err error) {
	standing, until, given = map[string]Answer{}, map[string]time.Time{}, map[string]time.Time{}
	if in.id == "" {
		return standing, until, given, nil
	}
	body, err := in.carried()
	if err != nil {
		return standing, until, given, err
	}
	var kept []StandingAnswer
	if json.Unmarshal(body, &kept) != nil {
		return standing, until, given, nil
	}
	for _, one := range slices.DeleteFunc(kept, heldInMemoryOnly) {
		if !one.Given.IsZero() {
			given[one.Target] = one.Given
		}
		switch one.Decision {
		case AllowAlways:
			standing[one.Target] = AlwaysHere
		case RejectAlways:
			standing[one.Target] = NeverHere
		case AllowFor:
			if time.Now().Before(one.Until) {
				standing[one.Target], until[one.Target] = AllowedAWhile, one.Until
			}
		}
	}
	return standing, until, given, nil
}

func (in standingIn) keep(standing map[string]Answer, until, given map[string]time.Time) error {
	if in.id == "" {
		return nil
	}
	body, err := json.MarshalIndent(slices.DeleteFunc(listed(standing, until, given), heldInMemoryOnly), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(in.store.Dir(in.id), 0o700); err != nil {
		return err
	}
	temporary := in.file() + ".tmp"
	if err := os.WriteFile(temporary, body, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, in.file())
}

func heldInMemoryOnly(one StandingAnswer) bool {
	return one.Decision != RejectAlways && loop.PlacesCode(one.Target)
}

func listed(standing map[string]Answer, until, given map[string]time.Time) []StandingAnswer {
	now := []StandingAnswer{}
	for _, place := range slices.Sorted(maps.Keys(standing)) {
		switch standing[place] {
		case AlwaysHere:
			now = append(now, StandingAnswer{Target: place, Decision: AllowAlways, Given: given[place]})
		case NeverHere:
			now = append(now, StandingAnswer{Target: place, Decision: RejectAlways, Given: given[place]})
		case AllowedAWhile:
			if time.Now().Before(until[place]) {
				now = append(now, StandingAnswer{Target: place, Decision: AllowFor, Given: given[place], Until: until[place]})
			}
		}
	}
	return now
}
