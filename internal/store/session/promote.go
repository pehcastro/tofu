package session

import (
	"encoding/json"
	"path/filepath"
	"time"
)

const PromotionsFileName = "promotions.jsonl"

type Place string

const (
	PlaceChat Place = "chat"
	PlaceWork Place = "work"
)

type PromotionAction string

const (
	ReachedIntoWork PromotionAction = "reached_into_work"
	MovedKind       PromotionAction = "moved_kind"
)

type Promotion struct {
	At        time.Time       `json:"at"`
	Session   string          `json:"session,omitempty"`
	Action    PromotionAction `json:"action"`
	EventID   string          `json:"event_id,omitempty"`
	EventKind string          `json:"event_kind"`
	FreeArm   Place           `json:"free_arm"`
	Chose     Place           `json:"chose"`
}

type PromotionLog string

func NewPromotionLog(stateDir string) PromotionLog {
	return PromotionLog(filepath.Join(stateDir, PromotionsFileName))
}

func (l PromotionLog) Append(row Promotion) error {
	if l == "" {
		return nil
	}
	line, err := json.Marshal(row)
	if err != nil {
		return err
	}
	return appendLines(string(l), append(line, '\n'))
}
