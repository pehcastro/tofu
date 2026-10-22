package host

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"tofu/internal/session"
	"tofu/internal/turn"
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

func (in standingIn) read() (map[string]Answer, map[string]time.Time, error) {
	standing, until := map[string]Answer{}, map[string]time.Time{}
	if in.id == "" {
		return standing, until, nil
	}
	body, err := os.ReadFile(in.file())
	if err != nil {
		return standing, until, err
	}
	var kept []StandingAnswer
	if json.Unmarshal(body, &kept) != nil {
		return standing, until, nil
	}
	for _, one := range slices.DeleteFunc(kept, heldInMemoryOnly) {
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
	return standing, until, nil
}

func (in standingIn) keep(standing map[string]Answer, until map[string]time.Time) error {
	if in.id == "" {
		return nil
	}
	body, err := json.MarshalIndent(slices.DeleteFunc(listed(standing, until), heldInMemoryOnly), "", "  ")
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
	return one.Decision != RejectAlways && turn.PlacesCode(one.Target)
}

func listed(standing map[string]Answer, until map[string]time.Time) []StandingAnswer {
	now := []StandingAnswer{}
	for _, place := range slices.Sorted(maps.Keys(standing)) {
		switch standing[place] {
		case AlwaysHere:
			now = append(now, StandingAnswer{Target: place, Decision: AllowAlways})
		case NeverHere:
			now = append(now, StandingAnswer{Target: place, Decision: RejectAlways})
		case AllowedAWhile:
			if time.Now().Before(until[place]) {
				now = append(now, StandingAnswer{Target: place, Decision: AllowFor, Until: until[place]})
			}
		}
	}
	return now
}
