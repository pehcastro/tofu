package trail

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/konst"
	"tofu/internal/store/session"
	"tofu/internal/tools/history/shadow"
)

const shortBlob = 7

func changeID(t shadow.Recorded, c shadow.Change) string { return t.ID + ":" + c.Path }

func ended(ctx context.Context, store shadow.Store) ([]shadow.Recorded, error) {
	recorded, err := store.Recorded(ctx, konst.ShadowTurnsKept)
	return slices.DeleteFunc(recorded, func(one shadow.Recorded) bool { return !one.Ended() }), err
}

func now(ctx context.Context, store shadow.Store, path string) (string, error) {
	if _, err := os.Lstat(filepath.Join(store.Work(), filepath.FromSlash(path))); errors.Is(err, fs.ErrNotExist) {
		return shadow.AbsentBlob, nil
	}
	hash, err := store.Git(ctx, "", "hash-object", "--", path)
	return strings.TrimSpace(hash), err
}

type told struct {
	session string
	turn    string
	text    string
}

func task(said []told, turnID string) string {
	first := slices.IndexFunc(said, func(s told) bool { return s.turn == turnID })
	if first < 0 {
		return "none recorded"
	}
	line, _, _ := strings.Cut(strings.TrimSpace(said[first].text), "\n")
	if len(line) > konst.CarrySaidMessageBytes {
		line = strings.ToValidUTF8(line[:konst.CarrySaidMessageBytes], "") + "..."
	}
	return strconv.Quote(line)
}

func spoken(sessions *session.Store, turns []shadow.Recorded) ([]told, error) {
	var seen []string
	var all []told
	for _, t := range turns {
		if slices.Contains(seen, t.Session) {
			continue
		}
		seen = append(seen, t.Session)
		events, err := sessions.Events(t.Session)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			var body struct {
				Task    string `json:"task"`
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			switch event.Kind {
			case session.EventTurnStart:
				if json.Unmarshal(event.Body, &body) == nil {
					all = append(all, told{t.Session, event.Turn, body.Task})
				}
			case session.EventMessage:
				if json.Unmarshal(event.Body, &body) == nil && (body.Role == session.RoleUser || body.Role == session.RoleAssistant) {
					all = append(all, told{t.Session, event.Turn, body.Content})
				}
			}
		}
	}
	return all, nil
}

func short(blob string) string {
	if blob == shadow.AbsentBlob {
		return "absent"
	}
	return blob[:min(len(blob), shortBlob)]
}
