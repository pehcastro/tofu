package session

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const lifetimeDay = 24 * time.Hour

type Lifetime int

const LifetimeNever Lifetime = 0

func ParseLifetime(text string) (Lifetime, error) {
	trimmed := strings.TrimSpace(strings.ToLower(text))
	if trimmed == "never" {
		return LifetimeNever, nil
	}
	days, err := strconv.Atoi(strings.TrimSuffix(trimmed, "d"))
	if err != nil || days < 0 {
		return 0, fmt.Errorf("session: %q is not a lifetime: 30d, 60d, 90d or never", text)
	}
	return Lifetime(days), nil
}

func (l Lifetime) String() string {
	if l == LifetimeNever {
		return "never"
	}
	return strconv.Itoa(int(l)) + "d"
}

func (l Lifetime) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }

func (l *Lifetime) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	parsed, err := ParseLifetime(text)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

func (l Lifetime) Expired(last, now time.Time) bool {
	if l == LifetimeNever {
		return false
	}
	return now.Sub(last) > time.Duration(l)*lifetimeDay
}

type Settings struct {
	RecordReads     bool     `json:"record_reads"`
	RecordReasoning bool     `json:"record_reasoning"`
	Lifetime        Lifetime `json:"lifetime"`
}

func DefaultSettings() Settings {
	return Settings{RecordReads: true, RecordReasoning: true, Lifetime: LifetimeNever}
}
