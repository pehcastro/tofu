package wrongpath

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"tofu/bench/corpus"
)

type Counts struct {
	Reverts          int
	ExactUndos       int
	ReReads          int
	IdenticalReReads int
	Contradictions   int
}

type callArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	Content   string `json:"content"`
}

type readRange struct {
	Start int
	End   int
	Bytes int64
}

func (r readRange) overlaps(other readRange) bool {
	if r.End == 0 || other.End == 0 {
		return true
	}
	return r.Start <= other.End && other.Start <= r.End
}

type mutation struct {
	before string
	after  string
}

func callsOf(session corpus.Turn) []corpus.RecordedCall {
	var calls []corpus.RecordedCall
	for _, step := range session.Steps {
		calls = append(calls, step.ToolCalls...)
	}
	return calls
}

func count(calls []corpus.RecordedCall) Counts {
	counts := Counts{}
	readsSinceMutation := map[string][]readRange{}
	mutations := map[string][]mutation{}
	previousFailed, anyPrevious := false, false
	for _, call := range calls {
		var args callArgs
		if len(call.Args) > 0 {
			_ = json.Unmarshal(call.Args, &args)
		}
		path := normalizePath(args.Path)
		switch {
		case call.Tool == "read" && path != "":
			this := readRange{Start: args.StartLine, End: args.EndLine, Bytes: call.ResultBytes}
			for _, earlier := range readsSinceMutation[path] {
				if !earlier.overlaps(this) {
					continue
				}
				counts.ReReads++
				if earlier == this {
					counts.IdenticalReReads++
				}
				break
			}
			readsSinceMutation[path] = append(readsSinceMutation[path], this)
		case (call.Tool == "write" || call.Tool == "edit") && path != "":
			this := mutation{before: args.OldString, after: args.NewString + args.Content}
			if len(mutations[path]) > 0 {
				counts.Reverts++
				for _, earlier := range mutations[path] {
					if this.before == earlier.after && this.after == earlier.before {
						counts.ExactUndos++
						break
					}
				}
			}
			mutations[path] = append(mutations[path], this)
			delete(readsSinceMutation, path)
		}
		failed := call.Error != "" || (call.ExitCode != nil && *call.ExitCode != 0)
		if failed && anyPrevious && !previousFailed {
			counts.Contradictions++
		}
		previousFailed, anyPrevious = failed, true
	}
	return counts
}

func normalizePath(path string) string {
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}
