package walls

import (
	"encoding/json"
	"strings"
	"time"

	"tofu/internal/judge/ledger"
	"tofu/internal/store/session"
)

type legacyBody struct {
	Event            string `json:"event"`
	Model            string `json:"model"`
	Error            string `json:"error"`
	Exit             int    `json:"exit"`
	Text             string `json:"text"`
	DurationMS       int64  `json:"duration_ms"`
	FirstTokenMS     int64  `json:"first_token_ms"`
	LastByteMS       int64  `json:"last_byte_ms"`
	PermissionWaitMS int64  `json:"permission_wait_ms"`
	WallClockMS      int64  `json:"wall_clock_ms"`
	Fork             *struct {
		Kind          string `json:"kind"`
		BlockedMicros int64  `json:"blocked_micros"`
	} `json:"fork"`
	Advisor *struct {
		Agent      string `json:"agent"`
		Error      string `json:"error"`
		DurationMS int64  `json:"duration_ms"`
	} `json:"advisor"`
}

func legacyEvent(event session.Event, id, tool string) Reading {
	var body legacyBody
	if json.Unmarshal(event.Body, &body) != nil {
		return Reading{Bad: 1}
	}
	var walls []Wall
	took := func(op Op, name string, ms int64, ok bool) {
		walls = append(walls, Wall{Op: op, Name: name, MS: ms, At: event.At.UnixMilli(), Session: id, OK: ok})
	}
	tookSome := func(op Op, name string, ms int64, ok bool) {
		if ms > 0 {
			took(op, name, ms, ok)
		}
	}
	switch event.Kind {
	case session.EventToolResult:
		took(OpTool, tool, body.DurationMS, body.Error == "")
		tookSome(OpPermissionWait, tool, body.PermissionWaitMS, true)
	case session.EventRequest:
		tookSome(OpRequest, body.Model, body.DurationMS, body.Error == "")
		tookSome(OpFirstToken, body.Model, body.FirstTokenMS, body.Error == "")
		tookSome(OpLastByte, body.Model, body.LastByteMS, body.Error == "")
	case session.EventTurnEnd:
		took(OpTurn, agentType(event.Agent), body.WallClockMS, body.Error == "")
	case session.EventHook:
		took(OpHook, body.Event, body.DurationMS, body.Exit == 0)
	case session.EventStep:
		if body.Fork != nil {
			took(OpForkBlocked, body.Fork.Kind, (time.Duration(body.Fork.BlockedMicros) * time.Microsecond).Milliseconds(), true)
		}
	case session.EventNotice:
		if body.Advisor != nil {
			took(OpAdvisor, agentType(body.Advisor.Agent), body.Advisor.DurationMS, body.Advisor.Error == "")
		}
		for _, notice := range legacyNotice(body.Text) {
			took(notice.Op, notice.Name, notice.MS, true)
		}
	}
	return Reading{Walls: walls}
}

func agentType(agent string) string {
	if agent == "" {
		return "lead"
	}
	return strings.TrimSuffix(strings.TrimRight(agent, "0123456789"), "-")
}

func legacyNotice(text string) []Wall {
	head, steps, _ := strings.Cut(text, ": ")
	before, total, timed := strings.Cut(head, " took ")
	total, _, _ = strings.Cut(total, " ")
	whole, err := time.ParseDuration(total)
	if !timed || err != nil {
		return nil
	}
	var op, stepOp Op
	var name, separator string
	switch spawned, isSpawn := strings.CutPrefix(before, "spawn of "); {
	case before == "startup":
		op, stepOp, name, separator = OpStartup, OpStartupStep, "to first request", " · "
	case isSpawn:
		op, stepOp, name, separator = OpSpawn, OpSpawnStep, agentType(spawned), ", "
	default:
		return nil
	}
	walls := []Wall{{Op: op, Name: name, MS: whole.Milliseconds()}}
	for _, step := range strings.Split(steps, separator) {
		at := strings.LastIndexByte(step, ' ')
		if took, err := time.ParseDuration(step[at+1:]); at > 0 && err == nil {
			walls = append(walls, Wall{Op: stepOp, Name: step[:at], MS: took.Milliseconds()})
		}
	}
	return walls
}

func legacyExchanges(store *session.Store, id string, since time.Time) Reading {
	exchanges, err := store.Exchanges(id)
	var reading Reading
	if err != nil {
		reading.Unread++
	}
	for _, exchange := range exchanges {
		for _, attempt := range exchange.Attempts {
			var detail struct {
				At        time.Time `json:"at"`
				Status    int       `json:"status"`
				Error     string    `json:"error"`
				HeadersMS int64     `json:"headers_ms"`
				StreamMS  int64     `json:"stream_ms"`
			}
			if json.Unmarshal(attempt.Detail, &detail) != nil {
				reading.Bad++
				continue
			}
			if detail.At.Before(since) || detail.Status == 0 {
				continue
			}
			wall := Wall{Op: OpHTTPHeaders, Name: exchange.Model, MS: detail.HeadersMS, At: detail.At.UnixMilli(), Session: id, OK: detail.Error == ""}
			reading.Walls = append(reading.Walls, wall)
			if detail.StreamMS > 0 {
				wall.Op, wall.MS = OpHTTPStream, detail.StreamMS
				reading.Walls = append(reading.Walls, wall)
			}
		}
	}
	return reading
}

func legacyLedger(dir string, since time.Time) (Reading, error) {
	var reading Reading
	report, err := ledger.NewReader(dir).Each(ledger.Filter{Since: since}, func(row ledger.Row) error {
		reading.Walls = append(reading.Walls, Wall{Op: OpJev, Name: row.Point, MS: row.LatencyMS, At: row.At.UnixMilli(), Session: row.SessionID, OK: true})
		return nil
	})
	reading.Bad += len(report.Corrupt)
	return reading, err
}
