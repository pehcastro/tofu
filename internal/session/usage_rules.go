package session

import (
	"encoding/json"
	"strings"
	"time"
)

const (
	ruleMarkOpen  = "from the rule "
	ruleMarkClose = "]"
)

type Week [daysInAWeek]int

type RuleFires struct {
	Fires int  `json:"fires"`
	Week  Week `json:"fires_week"`
}

func (s *Store) RuleFires(now time.Time) (map[string]RuleFires, []Skip, error) {
	listing, err := s.Listing()
	if err != nil {
		return nil, nil, err
	}
	fires, skipped := map[string]RuleFires{}, listing.Skipped
	for _, header := range listing.Sessions {
		events, err := s.Events(header.ID)
		if err != nil {
			skipped = append(skipped, Skip{ID: header.ID, Reason: err})
			continue
		}
		for _, event := range events {
			var sent struct {
				System  string `json:"system"`
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if event.Kind != EventPrompt && event.Kind != EventMessage || json.Unmarshal(event.Body, &sent) != nil || event.Kind == EventMessage && sent.Role != RoleUser {
				continue
			}
			ago := int(calendarDay(now, now).Sub(calendarDay(event.At, now)).Hours() / hoursInADay)
			named := map[string]bool{}
			for _, part := range strings.Split(sent.System+sent.Content, ruleMarkOpen)[1:] {
				id, closed := strings.CutSuffix(strings.SplitAfter(part, ruleMarkClose)[0], ruleMarkClose)
				if !closed || named[id] {
					continue
				}
				named[id] = true
				fired := fires[id]
				fired.Fires++
				if ago < daysInAWeek {
					fired.Week[daysInAWeek-1-ago]++
				}
				fires[id] = fired
			}
		}
	}
	return fires, skipped, nil
}

func calendarDay(at, now time.Time) time.Time {
	year, month, day := at.In(now.Location()).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
