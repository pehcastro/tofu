package prompts

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/konst"
)

var readingTools = map[string]bool{
	"read":           true,
	"grep":           true,
	"glob":           true,
	"search":         true,
	"artifact_fetch": true,
}

var shellReadVerbs = map[string]bool{
	"cat":  true,
	"head": true,
	"tail": true,
	"less": true,
	"ls":   true,
	"tree": true,
	"find": true,
	"rg":   true,
	"grep": true,
	"sed":  true,
	"wc":   true,
}

type Session struct {
	ID             string
	Task           string
	Calls          int
	ReadCalls      int
	ReadTokens     int64
	Retreats       int
	Contradictions int
}

func (s Session) WrongDirections() int {
	return s.Retreats + s.Contradictions
}

func shellReads(command string) bool {
	for _, piece := range strings.FieldsFunc(command, func(r rune) bool {
		return r == '|' || r == ';' || r == '&' || r == '\n'
	}) {
		fields := strings.Fields(piece)
		if len(fields) > 0 && shellReadVerbs[fields[0]] {
			return true
		}
	}
	return false
}

func mutatedPath(call corpus.RecordedCall) string {
	if call.Tool != "write" && call.Tool != "edit" {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	if len(call.Args) > 0 {
		_ = json.Unmarshal(call.Args, &args)
	}
	if args.Path == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(args.Path)))
}

func measure(turn corpus.Turn) Session {
	session := Session{ID: turn.ID, Task: turn.Task}
	mutated := map[string]bool{}
	previousSucceeded := false
	for _, step := range turn.Steps {
		for _, call := range step.ToolCalls {
			session.Calls++
			if readingTools[call.Tool] || (call.Tool == "bash" && shellReads(call.Command)) {
				session.ReadCalls++
				rendered := call.RenderedBytes
				if rendered == 0 {
					rendered = call.ResultBytes
				}
				session.ReadTokens += rendered / int64(konst.SearchBytesPerToken)
			}
			if path := mutatedPath(call); path != "" {
				if mutated[path] {
					session.Retreats++
				}
				mutated[path] = true
			}
			failed := call.Error != "" || (call.ExitCode != nil && *call.ExitCode != 0)
			if failed && previousSucceeded {
				session.Contradictions++
			}
			previousSucceeded = !failed
		}
	}
	return session
}
