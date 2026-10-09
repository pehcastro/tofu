package boardy

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

type SprintState string

const (
	Planned SprintState = "planned"
	Active  SprintState = "active"
	Closed  SprintState = "closed"
)

func parseSprintState(raw string) (SprintState, error) {
	switch state := SprintState(raw); state {
	case Planned, Active, Closed:
		return state, nil
	}
	return "", fmt.Errorf("%q is not a sprint state: planned, active, closed", raw)
}

type Sprint struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	State SprintState `json:"state"`
	Start time.Time   `json:"start,omitzero"`
	End   time.Time   `json:"end,omitzero"`
	Text  string      `json:"text,omitempty"`
}

func (s Store) Sprints(key string) ([]Sprint, error) {
	files, err := s.planFiles(key, "sprints")
	sprints := make([]Sprint, 0, len(files))
	for _, file := range files {
		state, stateErr := parseSprintState(file.fields["state"])
		start, startErr := parseDay(file.fields["start"])
		end, endErr := parseDay(file.fields["end"])
		if fileErr := errors.Join(stateErr, startErr, endErr); fileErr != nil {
			err = errors.Join(err, fmt.Errorf("sprints/%s.md: %w", file.fields["id"], fileErr))
			continue
		}
		sprints = append(sprints, Sprint{ID: file.fields["id"], Title: file.fields["title"], State: state, Start: start, End: end, Text: file.text})
	}
	return sprints, err
}

func (s Store) SaveSprint(key string, sprint Sprint) error {
	return s.savePlan(key, "sprints", sprint.ID, sprint.Text, "title", sprint.Title, "state", string(sprint.State), "start", formatDay(sprint.Start), "end", formatDay(sprint.End))
}

func ActiveSprint(sprints []Sprint) (Sprint, bool) {
	for _, sprint := range sprints {
		if sprint.State == Active {
			return sprint, true
		}
	}
	return Sprint{}, false
}

func (s Store) SetSprint(key, id string, state SprintState) error {
	sprints, err := s.Sprints(key)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(sprints, func(sprint Sprint) bool { return sprint.ID == id }) {
		return fmt.Errorf("the board %s has no sprint %s", key, id)
	}
	for _, sprint := range sprints {
		switch {
		case sprint.ID == id:
			sprint.State = state
			if state == Active && sprint.Start.IsZero() {
				sprint.Start = time.Now().UTC().Truncate(24 * time.Hour)
			}
		case state == Active && sprint.State == Active:
			sprint.State = Closed
		default:
			continue
		}
		if err := s.SaveSprint(key, sprint); err != nil {
			return err
		}
	}
	return nil
}
