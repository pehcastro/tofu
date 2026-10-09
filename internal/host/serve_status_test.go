package host

import (
	"bufio"
	"encoding/json"
	"io"
	"strconv"
	"testing"
	"time"

	"tofu/internal/cron"
)

type wireLine struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *Refusal        `json:"error"`
}

type wireClient struct {
	t     *testing.T
	in    *io.PipeWriter
	lines chan wireLine
	seen  []string
}

func (c *wireClient) ask(id, method, params string) {
	if _, err := io.WriteString(c.in, `{"jsonrpc":"2.0","id":"`+id+`","method":"`+method+`","params":`+params+"}\n"); err != nil {
		c.t.Fatal(err)
	}
}

func (c *wireClient) until(wanted func(wireLine) bool, what string) wireLine {
	c.t.Helper()
	gaveUp := time.After(5 * time.Second)
	for {
		select {
		case line := <-c.lines:
			c.seen = append(c.seen, line.Method+string(line.ID))
			if wanted(line) {
				return line
			}
		case <-gaveUp:
			c.t.Fatalf("never saw %s; saw %v", what, c.seen)
		}
	}
}

func (c *wireClient) answer(id string, into any) {
	c.t.Helper()
	line := c.until(func(line wireLine) bool { return string(line.ID) == `"`+id+`"` }, "the answer to "+id)
	if line.Error != nil {
		c.t.Fatalf("%s was refused: %s", id, line.Error.Message)
	}
	if err := json.Unmarshal(line.Result, into); err != nil {
		c.t.Fatalf("%s answered %s: %v", id, line.Result, err)
	}
}

func (c *wireClient) cronUpdated(live int) CronState {
	c.t.Helper()
	var state CronState
	c.until(func(line wireLine) bool {
		return line.Method == "cron.updated" && json.Unmarshal(line.Params, &state) == nil && state.Live == live
	}, "cron.updated with live "+strconv.Itoa(live))
	return state
}

func serving(t *testing.T, play Play, cfg ServeConfig) (*wireClient, *Host, string) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	h, _ := New(Config{Dir: project, Play: play})
	t.Cleanup(h.Close)
	return servingHost(t, h, project, cfg), h, project
}

func servingHost(t *testing.T, h *Host, project string, cfg ServeConfig) *wireClient {
	clientIn, serveIn := io.Pipe()
	serveOut, clientOut := io.Pipe()
	c := &wireClient{t: t, in: serveIn, lines: make(chan wireLine, 64)}
	go func() {
		lines := bufio.NewScanner(serveOut)
		for lines.Scan() {
			var line wireLine
			if json.Unmarshal(lines.Bytes(), &line) == nil {
				c.lines <- line
			}
		}
	}()
	served := make(chan error, 1)
	go func() {
		cfg.Host, cfg.Dir, cfg.In, cfg.Out = h, project, clientIn, clientOut
		served <- Serve(cfg)
	}()
	t.Cleanup(func() {
		_ = serveIn.Close()
		<-served
		_ = serveOut.Close()
	})
	return c
}

func TestServeTellsCronJobsAndQuotaWithoutATurn(t *testing.T) {
	c, h, _ := serving(t, nil, ServeConfig{Quota: func() []QuotaWindow {
		return []QuotaWindow{{Account: "#1", Window: "claude-sub 5h", Percent: 34, Reported: true}}
	}})

	c.ask("1", "initialize", `{"client":"scratch"}`)
	c.ask("2", "session.open", `{}`)
	opened := c.until(func(line wireLine) bool { return line.Method == "quota.updated" || line.Method == "turn.started" }, "quota.updated after session.open")
	if opened.Method != "quota.updated" {
		t.Fatalf("a turn started before any quota.updated, so the account item is empty until a turn ends")
	}

	var before CronState
	c.ask("3", "query.cron", `{}`)
	c.answer("3", &before)
	if before.Live != 0 || before.Jobs == nil || len(before.Jobs) != 0 {
		t.Fatalf("query.cron before any job = %+v, want live 0 and an empty list rather than null", before)
	}

	c.ask("4", "cron.command", `{"line":"/loop 10m check the build"}`)
	added := c.cronUpdated(1)
	if len(added.Jobs) != 1 || added.Jobs[0].ID != "c1" || added.Jobs[0].Next == nil || added.Jobs[0].Paused {
		t.Fatalf("cron.updated after /loop = %+v, want c1 live with a next fire", added)
	}

	agentMade, err := h.Cron().Create(cron.Spec{Schedule: "every 15m", Prompt: "check the docs"}, cron.Agent, "the agent was asked", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if byAgent := c.cronUpdated(2); byAgent.Jobs[1].ID != agentMade.ID {
		t.Fatalf("a job the agent made mid-turn reached the client as %+v, want %s second", byAgent, agentMade.ID)
	}

	c.ask("5", "cron.command", `{"line":"/cron pause c1"}`)
	if paused := c.cronUpdated(1); !paused.Jobs[0].Paused {
		t.Fatalf("after /cron pause c1, c1 = %+v, want paused", paused.Jobs[0])
	}

	var after CronState
	c.ask("6", "query.cron", `{}`)
	c.answer("6", &after)
	if after.Live != 1 || len(after.Jobs) != 2 {
		t.Fatalf("query.cron after the changes = %+v, want two jobs and one live", after)
	}

	quiet := time.After(1500 * time.Millisecond)
	for {
		select {
		case line := <-c.lines:
			if line.Method == "cron.updated" {
				t.Fatalf("cron.updated with nothing changed, so a client is told the same list every poll: %s", line.Params)
			}
		case <-quiet:
			return
		}
	}
}
