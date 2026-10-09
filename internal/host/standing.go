package host

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"tofu/internal/session"
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

func (in standingIn) read() (map[string]Answer, error) {
	standing := map[string]Answer{}
	if in.id == "" {
		return standing, nil
	}
	body, err := os.ReadFile(in.file())
	if err != nil {
		return standing, err
	}
	var kept []StandingAnswer
	if json.Unmarshal(body, &kept) != nil {
		return standing, nil
	}
	for _, one := range kept {
		switch one.Decision {
		case AllowAlways:
			standing[one.Target] = AlwaysHere
		case RejectAlways:
			standing[one.Target] = NeverHere
		}
	}
	return standing, nil
}

func (in standingIn) keep(standing map[string]Answer) error {
	if in.id == "" {
		return nil
	}
	body, err := json.MarshalIndent(listed(standing), "", "  ")
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

func listed(standing map[string]Answer) []StandingAnswer {
	now := []StandingAnswer{}
	for _, place := range slices.Sorted(maps.Keys(standing)) {
		decision := AllowAlways
		if standing[place] == NeverHere {
			decision = RejectAlways
		}
		now = append(now, StandingAnswer{Target: place, Decision: decision})
	}
	return now
}
