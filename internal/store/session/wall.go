package session

import (
	"encoding/json"
	"errors"
)

type WallEvent struct {
	Op        string `json:"op"`
	Name      string `json:"name"`
	MS        int64  `json:"ms"`
	OutsideMS *int64 `json:"outside_ms,omitempty"`
	Turn      string `json:"-"`
	Agent     string `json:"-"`
	Request   string `json:"-"`
	Call      string `json:"-"`
}

func DecodeWall(event Event) (WallEvent, error) {
	if event.Kind != EventWall {
		return WallEvent{}, errors.New("not a wall event: " + string(event.Kind))
	}
	wall := WallEvent{Turn: event.Turn, Agent: event.Agent, Request: event.Request, Call: event.Call}
	return wall, json.Unmarshal(event.Body, &wall)
}
