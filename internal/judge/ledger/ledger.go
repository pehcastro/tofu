package ledger

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"boji/internal/sys"
)

const SchemaVersion = 5

const dayLayout = "2006-01-02"

type Verdict string

const (
	VerdictUnset Verdict = ""
	VerdictAllow Verdict = "allow"
	VerdictAsk   Verdict = "ask"
	VerdictDeny  Verdict = "deny"
)

func (v Verdict) String() string {
	switch v {
	case VerdictUnset:
		return "unset"
	case VerdictAllow:
		return "allow"
	case VerdictAsk:
		return "ask"
	case VerdictDeny:
		return "deny"
	}
	panic("ledger: unknown verdict " + string(v))
}

type Slice struct {
	Option string  `json:"option"`
	P      float64 `json:"p"`
}

type AnswerKind string

const (
	AnswerNoul   AnswerKind = "noul"
	AnswerChoice AnswerKind = "choice"
	AnswerScore  AnswerKind = "score"
)

type Answer struct {
	Question string     `json:"question"`
	Wording  int        `json:"wording"`
	Kind     AnswerKind `json:"kind"`
	Noul     float64    `json:"noul,omitempty"`
	Choice   string     `json:"choice,omitempty"`
	Score    float64    `json:"score,omitempty"`
	Dist     []Slice    `json:"dist,omitempty"`
}

type answerWire struct {
	Question string      `json:"question"`
	Wording  int         `json:"wording"`
	Kind     *AnswerKind `json:"kind"`
	Noul     *float64    `json:"noul"`
	Choice   *string     `json:"choice"`
	Score    *float64    `json:"score"`
	Dist     []Slice     `json:"dist"`
}

func (a *Answer) UnmarshalJSON(data []byte) error {
	var wire answerWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Kind != nil {
		return a.fromTypedWire(wire)
	}
	return a.fromLegacyWire(wire)
}

func (a *Answer) fromTypedWire(wire answerWire) error {
	*a = Answer{Question: wire.Question, Wording: wire.Wording, Kind: *wire.Kind, Dist: wire.Dist}
	switch *wire.Kind {
	case AnswerNoul:
		if wire.Noul != nil {
			a.Noul = *wire.Noul
		}
	case AnswerChoice:
		if wire.Choice != nil {
			a.Choice = *wire.Choice
		}
	case AnswerScore:
		if wire.Score != nil {
			a.Score = *wire.Score
		}
	default:
		return fmt.Errorf("ledger: answer %q carries unknown kind %q", wire.Question, *wire.Kind)
	}
	return nil
}

func (a *Answer) fromLegacyWire(wire answerWire) error {
	choice := ""
	if wire.Choice != nil {
		choice = *wire.Choice
	}
	if len(wire.Dist) == 0 {
		p, err := strconv.ParseFloat(choice, 64)
		if err != nil {
			return fmt.Errorf("ledger: legacy noul answer %q: %w", wire.Question, err)
		}
		*a = Answer{Question: wire.Question, Wording: wire.Wording, Kind: AnswerNoul, Noul: p}
		return nil
	}
	if looksLikeScoreDist(wire.Dist) {
		level, _ := strconv.ParseFloat(choice, 64)
		*a = Answer{Question: wire.Question, Wording: wire.Wording, Kind: AnswerScore, Score: level, Dist: wire.Dist}
		return nil
	}
	*a = Answer{Question: wire.Question, Wording: wire.Wording, Kind: AnswerChoice, Choice: choice, Dist: wire.Dist}
	return nil
}

func looksLikeScoreDist(dist []Slice) bool {
	for _, slice := range dist {
		if _, err := strconv.Atoi(slice.Option); err != nil {
			return false
		}
	}
	return true
}

type Mode string

const (
	ModeUnknown  Mode = ""
	ModeShadow   Mode = "shadow"
	ModeEnforced Mode = "enforced"
)

func (m Mode) String() string {
	switch m {
	case ModeUnknown:
		return "unknown"
	case ModeShadow:
		return "shadow"
	case ModeEnforced:
		return "enforced"
	}
	panic("ledger: unknown mode " + string(m))
}

type Outcome struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail,omitempty"`
}

type Reason struct {
	Question   string  `json:"question"`
	Comparison string  `json:"comparison"`
	Threshold  float64 `json:"threshold"`
	Value      float64 `json:"value"`
	DeadBand   bool    `json:"dead_band,omitempty"`
	RelaxedBy  string  `json:"relaxed_by,omitempty"`
	Blocked    bool    `json:"blocked,omitempty"`
	Ambiguous  string  `json:"ambiguous,omitempty"`
	Mode       Mode    `json:"mode,omitempty"`
	ModeReason *string `json:"mode_reason,omitempty"`
}

type Row struct {
	ID            string    `json:"id"`
	Schema        int       `json:"schema"`
	At            time.Time `json:"at"`
	Point         string    `json:"point"`
	Questions     string    `json:"questions"`
	Version       int       `json:"version"`
	Build         string    `json:"build"`
	Model         string    `json:"model"`
	StateHash     string    `json:"state_hash"`
	StateBuilder  string    `json:"state_builder,omitempty"`
	Answers       []Answer  `json:"answers"`
	Verdict       Verdict   `json:"verdict"`
	Policy        string    `json:"policy,omitempty"`
	PolicyVersion int       `json:"policy_version,omitempty"`
	Reason        *Reason   `json:"reason,omitempty"`
	LatencyMS     int64     `json:"latency_ms"`
	Cost          float64   `json:"cost"`
	RequestID     string    `json:"request_id"`
	ReplayOf      string    `json:"replay_of,omitempty"`
	Outcome       *Outcome  `json:"outcome,omitempty"`
	TurnID        string    `json:"turn_id,omitempty"`
}

func (r Row) Day() string {
	return r.At.UTC().Format(dayLayout)
}

func (r Row) Mode() Mode {
	if r.Reason == nil {
		return ModeUnknown
	}
	return r.Reason.Mode
}

func Dir() (string, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "log"), nil
}

func CacheDir() (string, error) {
	state, err := sys.ProjectStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "cache"), nil
}
