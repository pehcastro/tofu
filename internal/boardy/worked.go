package boardy

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/safe"
	"tofu/internal/session"
)

const (
	readTool          = "read"
	artifactFetchTool = "artifact_fetch"
)

type run struct {
	session, agent, model string
	tools, reads          int
}

type runEvent struct {
	Ticket  string    `json:"ticket"`
	Kind    EventKind `json:"kind"`
	Session string    `json:"session"`
	Agent   string    `json:"agent"`
	Author  *Author   `json:"author"`
}

type toolCall struct {
	Agent string            `json:"agent"`
	Kind  session.EventKind `json:"kind"`
	Body  session.CallBody  `json:"body"`
}

func eachLine(path string, keep func([]byte) bool, read func([]byte)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, konst.StreamLineBytes)
	for scanner.Scan() {
		if keep(scanner.Bytes()) {
			read(scanner.Bytes())
		}
	}
	return scanner.Err()
}

func (s Store) runs(key string, listed []Group) (map[string]run, error) {
	runs, wanted := map[string]run{}, map[string]bool{}
	for _, group := range listed {
		for _, ticket := range group.Tickets {
			wanted[ticket.ID] = true
		}
	}
	err := eachLine(filepath.Join(s.BoardDir(key), EventsFileName),
		func(line []byte) bool {
			return bytes.Contains(line, []byte(`"spawned"`)) || bytes.Contains(line, []byte(`"model"`))
		},
		func(line []byte) {
			var event runEvent
			if json.Unmarshal(line, &event) != nil || !wanted[event.Ticket] {
				return
			}
			last, seen := runs[event.Ticket]
			switch {
			case event.Kind == Spawned && event.Agent != "":
				runs[event.Ticket] = run{session: event.Session, agent: event.Agent}
			case seen && event.Author != nil && event.Author.Name == last.agent && event.Author.Model != "":
				last.model = event.Author.Model
				runs[event.Ticket] = last
			}
		})
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	bySession := map[string]map[string]run{}
	for _, worked := range runs {
		if worked.session != "" {
			bySession[worked.session] = map[string]run{}
		}
	}
	sessions := session.OpenAt(filepath.Dir(s.Dir))
	var reading sync.WaitGroup
	for id, agents := range bySession {
		reading.Go(func() {
			_ = safe.Call(s.crashed, "boardy rows session "+id, func() error { return agentsIn(sessions, id, agents) })
		})
	}
	reading.Wait()
	for ticket, worked := range runs {
		counted := bySession[worked.session][worked.agent]
		worked.model, worked.tools, worked.reads = cmp.Or(worked.model, counted.model), counted.tools, counted.reads
		runs[ticket] = worked
	}
	return runs, err
}

func agentsIn(sessions *session.Store, id string, agents map[string]run) error {
	err := eachLine(sessions.EventsPath(id), func(line []byte) bool { return bytes.Contains(line, []byte(`"kind":"tool_call"`)) }, func(line []byte) {
		var call toolCall
		if json.Unmarshal(line, &call) != nil || call.Kind != session.EventToolCall {
			return
		}
		counted := agents[call.Agent]
		counted.tools++
		if call.Body.Tool == readTool || call.Body.Tool == artifactFetchTool {
			counted.reads++
		}
		agents[call.Agent] = counted
	})
	if err != nil {
		return err
	}
	header, err := sessions.Header(id)
	for _, ran := range header.Agents {
		counted := agents[ran.Agent]
		counted.model = ran.Model
		agents[ran.Agent] = counted
	}
	return err
}
