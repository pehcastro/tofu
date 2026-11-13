package walls

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	platformproject "tofu/internal/platform/project"
	"tofu/internal/store/session"
)

type Reading struct {
	Walls  []Wall
	Bad    int
	Unread int
}

func (r *Reading) add(other Reading) {
	r.Walls, r.Bad, r.Unread = append(r.Walls, other.Walls...), r.Bad+other.Bad, r.Unread+other.Unread
}

func (r *Reading) keep(raw []byte, session string, since time.Time) {
	var wall Wall
	if json.Unmarshal(raw, &wall) != nil || !slices.Contains(Ops(), wall.Op) {
		r.Bad++
		return
	}
	if wall.At >= since.UnixMilli() {
		wall.Session = cmp.Or(wall.Session, session)
		r.Walls = append(r.Walls, wall)
	}
}

func ReadProject(places platformproject.Places, since time.Time) (Reading, error) {
	reading, err := readDays(filepath.Join(places.State, dirName), since)
	if err != nil {
		return reading, err
	}
	sessions, err := readSessions(places.State, since)
	if err != nil {
		return reading, err
	}
	reading.add(sessions)
	decisions, err := legacyLedger(places.Log, since)
	reading.add(decisions)
	return reading, err
}

func readDays(dir string, since time.Time) (Reading, error) {
	var reading Reading
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return reading, nil
	}
	if err != nil {
		return reading, err
	}
	firstDay := since.UTC().Format(dayLayout)
	for _, entry := range entries {
		day, isDay := strings.CutSuffix(entry.Name(), daySuffix)
		if !isDay || day < firstDay {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			reading.Unread++
			continue
		}
		for line := range bytes.Lines(raw) {
			if line = bytes.TrimSpace(line); len(line) > 0 {
				reading.keep(line, "", since)
			}
		}
	}
	return reading, nil
}

func readSessions(state string, since time.Time) (Reading, error) {
	entries, err := os.ReadDir(session.SessionsDir(state))
	if errors.Is(err, os.ErrNotExist) {
		return Reading{}, nil
	}
	if err != nil {
		return Reading{}, err
	}
	store := session.OpenAt(state)
	var touched []string
	for _, entry := range entries {
		if info, err := os.Stat(store.EventsPath(entry.Name())); entry.IsDir() && err == nil && !info.ModTime().Before(since) {
			touched = append(touched, entry.Name())
		}
	}
	readings := make([]Reading, len(touched))
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var all sync.WaitGroup
	for i, id := range touched {
		all.Go(func() {
			slots <- struct{}{}
			readings[i] = readSession(store, id, since)
			<-slots
		})
	}
	all.Wait()
	var reading Reading
	for _, one := range readings {
		reading.add(one)
	}
	return reading, nil
}

func readSession(store *session.Store, id string, since time.Time) Reading {
	var reading Reading
	events, err := store.EventsOf(id, session.EventWall, session.EventToolCall, session.EventToolResult, session.EventRequest,
		session.EventTurnEnd, session.EventHook, session.EventStep, session.EventNotice)
	if err != nil {
		reading.Unread++
	}
	tools := map[string]string{}
	for _, event := range events {
		switch {
		case event.Kind == session.EventToolCall:
			var call session.CallBody
			_ = json.Unmarshal(event.Body, &call)
			tools[event.Call] = call.Tool
		case event.At.Before(since):
		case event.Kind == session.EventWall:
			reading.keep(event.Body, id, since)
		default:
			reading.add(legacyEvent(event, id, tools[event.Call]))
		}
	}
	reading.add(legacyExchanges(store, id, since))
	return reading
}
