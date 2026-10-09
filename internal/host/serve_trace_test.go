package host

import (
	"encoding/json"
	"testing"
)

func TestServeSessionTraceCarriesMemoryModelCalls(t *testing.T) {
	printed := `{"session":"s1","handle":"h","events":4,"agents":[],"requests":[],"calls":[],"cache":{},` +
		`"memory":[{"request":"r9","at":"2026-10-09T10:00:00Z","why":"memory recall","model":"claude-sub/claude-haiku-5","usage":{"input_tokens":610,"output_tokens":12},"cost_usd":0}]}`
	verb := func(args []string) (VerbResult, error) {
		return VerbResult{OK: true, Data: json.RawMessage(printed)}, nil
	}
	c, _, _ := serving(t, nil, ServeConfig{Verb: verb})
	c.ask("0", "initialize", `{"client":"scratch"}`)
	c.answer("0", &InitializeResult{})
	c.ask("1", "session.trace", `{"session":"s1"}`)
	var traced struct {
		Memory []struct {
			Request string `json:"request"`
			Model   string `json:"model"`
		} `json:"memory"`
	}
	c.answer("1", &traced)
	if len(traced.Memory) != 1 || traced.Memory[0].Request != "r9" || traced.Memory[0].Model != "claude-sub/claude-haiku-5" {
		t.Errorf("session.trace answered memory %+v, want the one memory-model call the verb printed", traced.Memory)
	}
}
