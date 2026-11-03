package turn

import (
	"strings"
	"time"
)

type spawnTiming struct {
	began, last time.Time
	steps       []string
}

func startSpawnTiming() *spawnTiming {
	now := time.Now()
	return &spawnTiming{began: now, last: now}
}

func (s *spawnTiming) took(step string) {
	now := time.Now()
	s.steps = append(s.steps, step+" "+now.Sub(s.last).Round(time.Millisecond).String())
	s.last = now
}

func (s *spawnTiming) words(id string) string {
	return "spawn of " + id + " took " + time.Since(s.began).Round(time.Millisecond).String() + " before it ran: " + strings.Join(s.steps, ", ")
}
