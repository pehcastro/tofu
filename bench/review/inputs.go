package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	From string    `json:"from"`
	To   string    `json:"to"`
	Text string    `json:"text"`
}

type ticket struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	Status     string    `json:"status"`
	Epic       string    `json:"epic"`
	Labels     []string  `json:"labels"`
	Created    time.Time `json:"created"`
	Title      string    `json:"title"`
	Problem    string    `json:"problem"`
	Scope      string    `json:"scope"`
	Before     string    `json:"before"`
	Driven     string    `json:"driven"`
	StillWorks string    `json:"still_works"`
	ErrorCase  string    `json:"error_case"`
	Acceptance []string  `json:"acceptance"`
	Log        string    `json:"log"`
	Thread     []event   `json:"thread"`
}

type said struct {
	Generation int       `json:"generation"`
	At         time.Time `json:"at"`
	Speaker    string    `json:"speaker"`
	Text       string    `json:"text"`
}

type transcript struct {
	Handle  string `json:"handle"`
	Entries []said `json:"entries"`
}

type ruleRow struct {
	Turn       string `json:"turn"`
	Agent      string `json:"agent"`
	Rule       string `json:"rule"`
	ReasonKind string `json:"reason_kind"`
}

type libraryRule struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type inputs struct {
	tickets     []ticket
	threads     map[string][]event
	transcripts []transcript
	whyRows     []ruleRow
	library     []libraryRule
	read        map[string]int
}

type envelope struct {
	Tofu string          `json:"tofu"`
	Verb string          `json:"verb"`
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
}

func load(paths []string) (inputs, error) {
	in := inputs{threads: map[string][]event{}, read: map[string]int{}}
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
				return err
			}
			return in.add(path)
		})
		if err != nil {
			return in, err
		}
	}
	return in, nil
}

func (in *inputs) add(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var e envelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !e.OK {
		return fmt.Errorf("%s: %s answered ok false", path, e.Verb)
	}
	in.read["tofu "+e.Tofu+" "+e.Verb]++
	switch e.Verb {
	case "boardy list":
		var board struct{ Tickets []ticket }
		err = json.Unmarshal(e.Data, &board)
		in.tickets = append(in.tickets, board.Tickets...)
	case "boardy show":
		var shown ticket
		err = json.Unmarshal(e.Data, &shown)
		in.threads[shown.ID] = shown.Thread
	case "session transcript":
		var t transcript
		err = json.Unmarshal(e.Data, &t)
		in.transcripts = append(in.transcripts, t)
		in.read["transcript of "+t.Handle]++
	case "rules why":
		var why struct{ Rows []ruleRow }
		err = json.Unmarshal(e.Data, &why)
		in.whyRows = append(in.whyRows, why.Rows...)
	case "rules list":
		var list struct{ Rules []libraryRule }
		err = json.Unmarshal(e.Data, &list)
		in.library = append(in.library, list.Rules...)
	default:
		return fmt.Errorf("%s: verb %q is not an export bench/review reads", path, e.Verb)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
