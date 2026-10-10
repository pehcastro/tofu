package host

import (
	"encoding/json"
	"testing"
)

func TestServeTurnsOfAFreshSessionAreEmpty(t *testing.T) {
	c, _, _ := serving(t, nil, ServeConfig{})
	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	var opened SessionOpenResult
	c.answer("2", &opened)

	for id, params := range map[string]string{"3": `{}`, "4": `{"session":"` + opened.Session + `"}`} {
		c.ask(id, "session.turns", params)
		line := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id)
		var fresh map[string]json.RawMessage
		if line.Error != nil || json.Unmarshal(line.Result, &fresh) != nil || string(fresh["session"]) != `"`+opened.Session+`"` || string(fresh["turns"]) != `[]` || string(fresh["snapshots"]) != `0` {
			t.Errorf("session.turns %s on a session with no turn yet answered %s and %+v, want the session, turns [] and snapshots 0", params, line.Result, line.Error)
		}
	}

	c.ask("5", "session.turns", `{"session":"nothing-opened-here"}`)
	if stranger := c.until(func(line wireLine) bool { return string(line.ID) == `"5"` }, "the answer to 5"); stranger.Error == nil {
		t.Errorf("session.turns on a session this serve never opened answered %s, want an error", stranger.Result)
	}
}
