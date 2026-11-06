package lsp

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	platformproject "tofu/internal/platform/project"
)

const (
	severityError = 1
)

type Problem struct {
	Line    int
	Message string
}

type diagnosis struct {
	arrived int
	version int
	errors  []Problem
}

type Edit struct {
	Range   span   `json:"range"`
	NewText string `json:"newText"`
}

type Neighbour struct {
	Name string
	Line int
}

func PathKey(path string) string {
	if platformproject.FoldsCase() {
		return strings.ToLower(filepath.Clean(path))
	}
	return filepath.Clean(path)
}

type reported []struct {
	Range    span   `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
}

func (r reported) errors() []Problem {
	var errors []Problem
	for _, one := range r {
		if one.Severity <= severityError {
			errors = append(errors, Problem{Line: one.Range.Start.Line + 1, Message: one.Message})
		}
	}
	return errors
}

func (c *Client) diagnosed(params json.RawMessage) {
	var published struct {
		URI         string   `json:"uri"`
		Version     int      `json:"version"`
		Diagnostics reported `json:"diagnostics"`
	}
	if json.Unmarshal(params, &published) != nil {
		return
	}
	c.state.Lock()
	defer c.state.Unlock()
	c.clock++
	c.heard = c.clock
	c.checked[PathKey(uriPath(published.URI))] = diagnosis{arrived: c.clock, version: published.Version, errors: published.Diagnostics.errors()}
	close(c.arrived)
	c.arrived = make(chan struct{})
}

func (c *Client) Errors(ctx context.Context, paths []string) (map[string][]Problem, []string) {
	if c.pulls {
		found, missing := map[string][]Problem{}, []string(nil)
		for _, path := range paths {
			var answer struct {
				Items reported `json:"items"`
			}
			if err := c.call(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]string{"uri": fileURI(path)}}, &answer); err != nil {
				missing = append(missing, path)
				continue
			}
			found[path] = answer.Items.errors()
		}
		return found, missing
	}
	for {
		c.state.Lock()
		arrived, found, missing, latest := c.arrived, map[string][]Problem{}, []string(nil), 0
		for _, path := range paths {
			latest = max(latest, c.opened[PathKey(path)].sent)
		}
		for _, path := range paths {
			shown, got := c.opened[PathKey(path)], c.checked[PathKey(path)]
			if got.arrived > shown.sent && (got.version == 0 || got.version >= shown.version) || c.heard > latest {
				found[path] = got.errors
			} else {
				missing = append(missing, path)
			}
		}
		c.state.Unlock()
		settled := time.After(konst.LspDiagnosticsSettleMillis * time.Millisecond)
		if len(missing) > 0 {
			settled = nil
		}
		select {
		case <-arrived:
		case <-settled:
			return found, nil
		case <-ctx.Done():
			return found, missing
		case <-c.exited:
			return found, missing
		}
	}
}

func (c *Client) Rename(ctx context.Context, at Location, to string) (map[string][]Edit, error) {
	params := at.params()
	params["newName"] = to
	var answer struct {
		Changes         map[string][]Edit `json:"changes"`
		DocumentChanges []struct {
			Kind         string `json:"kind"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []Edit `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := c.call(ctx, "textDocument/rename", params, &answer); err != nil {
		return nil, err
	}
	edits := map[string][]Edit{}
	for uri, list := range answer.Changes {
		edits[uriPath(uri)] = append(edits[uriPath(uri)], list...)
	}
	for _, change := range answer.DocumentChanges {
		if change.Kind != "" {
			return nil, fmt.Errorf("%s would also %s a file to carry out this rename, and symbols changes text only", c.server.Program, change.Kind)
		}
		edits[uriPath(change.TextDocument.URI)] = append(edits[uriPath(change.TextDocument.URI)], change.Edits...)
	}
	return edits, nil
}

func Apply(text string, edits []Edit) string {
	starts := []int{0}
	for at := range len(text) {
		if text[at] == '\n' {
			starts = append(starts, at+1)
		}
	}
	offset := func(at position) int {
		if at.Line >= len(starts) {
			return len(text)
		}
		line, _, _ := strings.Cut(text[starts[at.Line]:], "\n")
		return starts[at.Line] + byteOffset(line, at.Character)
	}
	type splice struct {
		from, to int
		text     string
	}
	var splices []splice
	for _, edit := range edits {
		splices = append(splices, splice{offset(edit.Range.Start), offset(edit.Range.End), edit.NewText})
	}
	slices.SortFunc(splices, func(a, b splice) int { return cmp.Or(cmp.Compare(b.from, a.from), cmp.Compare(b.to, a.to)) })
	for _, one := range slices.Compact(splices) {
		text = text[:one.from] + one.text + text[one.to:]
	}
	return text
}

func (s span) holds(at Location) bool {
	here := position{Line: at.Line - 1, Character: at.Character}
	before := func(a, b position) bool { return a.Line < b.Line || a.Line == b.Line && a.Character <= b.Character }
	return before(s.Start, here) && before(here, s.End)
}

func (n symbolNode) named() *span { return cmp.Or(n.Chosen, n.reach()) }

func (c *Client) Neighbours(ctx context.Context, at Location) ([]Neighbour, error) {
	level, err := c.outline(ctx, at.Path)
	if err != nil {
		return nil, err
	}
	for {
		inside := slices.IndexFunc(level, func(node symbolNode) bool { return node.reach().holds(at) })
		if inside < 0 {
			break
		}
		if level[inside].named().holds(at) || len(level[inside].Children) == 0 {
			level = slices.Delete(slices.Clone(level), inside, inside+1)
			break
		}
		level = level[inside].Children
	}
	neighbours := make([]Neighbour, len(level))
	for i, node := range level {
		neighbours[i] = Neighbour{Name: node.Name, Line: node.named().Start.Line + 1}
	}
	return neighbours, nil
}
