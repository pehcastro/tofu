package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Stats struct {
	Rows    int
	Corrupt int
	Days    int
	First   time.Time
	Last    time.Time
}

type rowStamp struct {
	ID            textShape     `json:"id"`
	Schema        int           `json:"schema"`
	At            time.Time     `json:"at"`
	Point         textShape     `json:"point"`
	Questions     textShape     `json:"questions"`
	Version       int           `json:"version"`
	Build         textShape     `json:"build"`
	Model         textShape     `json:"model"`
	StateHash     textShape     `json:"state_hash"`
	Fingerprint   textShape     `json:"fingerprint"`
	StateBuilder  textShape     `json:"state_builder"`
	State         anyShape      `json:"state"`
	StateElision  elisionShape  `json:"state_elision"`
	Answers       []answerShape `json:"answers"`
	Verdict       Verdict       `json:"verdict"`
	Policy        textShape     `json:"policy"`
	PolicyVersion int           `json:"policy_version"`
	Reason        reasonShape   `json:"reason"`
	LatencyMS     int64         `json:"latency_ms"`
	OutsideMS     int64         `json:"outside_ms"`
	Cost          float64       `json:"cost"`
	Provider      textShape     `json:"provider"`
	Key           textShape     `json:"key"`
	Usage         Usage         `json:"usage"`
	Charge        Charge        `json:"charge"`
	RequestID     textShape     `json:"request_id"`
	ReplayOf      textShape     `json:"replay_of"`
	Outcome       outcomeShape  `json:"outcome"`
	TurnID        textShape     `json:"turn_id"`
	SessionID     textShape     `json:"session_id"`
	Bench         textShape     `json:"bench"`
}

type answerShape struct {
	Question string      `json:"question"`
	Wording  int         `json:"wording"`
	Kind     *AnswerKind `json:"kind"`
	Noul     float64     `json:"noul"`
	Choice   *string     `json:"choice"`
	Score    float64     `json:"score"`
	Dist     []struct {
		Option textShape `json:"option"`
		P      float64   `json:"p"`
	} `json:"dist"`
	Legend []textShape `json:"legend"`
}

type reasonShape struct {
	Question   textShape `json:"question"`
	Comparison textShape `json:"comparison"`
	Threshold  float64   `json:"threshold"`
	Value      float64   `json:"value"`
	DeadBand   bool      `json:"dead_band"`
	RelaxedBy  textShape `json:"relaxed_by"`
	AllowedBy  textShape `json:"allowed_by"`
	Blocked    bool      `json:"blocked"`
	Ambiguous  textShape `json:"ambiguous"`
	Mode       Mode      `json:"mode"`
	ModeReason textShape `json:"mode_reason"`
}

type elisionShape struct {
	Bytes int       `json:"bytes"`
	Head  textShape `json:"head"`
	Tail  textShape `json:"tail"`
	File  textShape `json:"file"`
}

type outcomeShape struct {
	At     time.Time `json:"at"`
	Kind   textShape `json:"kind"`
	Detail textShape `json:"detail"`
}

type textShape struct{}

func (*textShape) UnmarshalJSON(data []byte) error {
	if data[0] == '"' || string(data) == "null" {
		return nil
	}
	return errors.New("ledger: a text field holds something other than a string")
}

type anyShape struct{}

func (*anyShape) UnmarshalJSON([]byte) error { return nil }

func (s rowStamp) readable() error {
	for _, answer := range s.Answers {
		if err := answerReadable(answer.Question, answer.Kind, answer.Choice, len(answer.Dist)); err != nil {
			return err
		}
	}
	return readableSchema(s.Schema)
}

func Summary(dir string) (Stats, error) {
	reader := NewReader(dir)
	days, err := reader.days()
	if err != nil {
		return Stats{}, err
	}
	each, errs := make([]Stats, len(days)), make([]error, len(days))
	atOnce(len(days), func(i int) { each[i], errs[i] = reader.dayStats(days[i]) })
	if err := errors.Join(errs...); err != nil {
		return Stats{}, err
	}
	stats := Stats{Days: len(days)}
	for _, day := range each {
		stats.Rows, stats.Corrupt = stats.Rows+day.Rows, stats.Corrupt+day.Corrupt
		if day.Rows > 0 && (stats.First.IsZero() || day.First.Before(stats.First)) {
			stats.First = day.First
		}
		if day.Rows > 0 && (stats.Last.IsZero() || day.Last.After(stats.Last)) {
			stats.Last = day.Last
		}
	}
	return stats, nil
}

func (r *Reader) dayStats(day string) (Stats, error) {
	var stats Stats
	cut, err := eachLine(r.dayPath(day), func(_ int, line []byte) error {
		var stamp rowStamp
		if json.Unmarshal(line, &stamp) != nil || stamp.readable() != nil {
			stats.Corrupt++
			return nil
		}
		if stats.Rows == 0 || stamp.At.Before(stats.First) {
			stats.First = stamp.At
		}
		if stats.Rows == 0 || stamp.At.After(stats.Last) {
			stats.Last = stamp.At
		}
		stats.Rows++
		return nil
	})
	if cut != nil {
		stats.Corrupt++
	}
	return stats, err
}

func (s Stats) String() string {
	if s.Rows == 0 {
		return "empty"
	}
	line := fmt.Sprintf("%d rows over %d days, %s to %s",
		s.Rows, s.Days, s.First.UTC().Format(dayLayout), s.Last.UTC().Format(dayLayout))
	if s.Corrupt > 0 {
		line += fmt.Sprintf(", %d unreadable lines", s.Corrupt)
	}
	return line
}
