package turn

import (
	"encoding/json"
	"strings"

	"tofu/internal/llm"
	"tofu/internal/search"
)

type SearchLinkKind string

const (
	SearchLinkNone      SearchLinkKind = "none"
	SearchLinkHit       SearchLinkKind = "hit"
	SearchLinkUnoffered SearchLinkKind = "unoffered"
)

type SearchLink struct {
	SearchCallID string         `json:"search_call_id"`
	Offered      []string       `json:"offered,omitempty"`
	ActedCallID  string         `json:"acted_call_id,omitempty"`
	ActedPath    string         `json:"acted_path,omitempty"`
	Kind         SearchLinkKind `json:"kind"`
}

type actingCall struct {
	id, tool, path string
}

func actsOnAPath(tool string) bool {
	return tool == "read" || tool == "edit" || tool == "write"
}

func slashed(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}

func callsInOrder(conversation []llm.Message) ([]actingCall, map[string]string) {
	var calls []actingCall
	results := make(map[string]string)
	for _, message := range conversation {
		for _, call := range message.ToolCalls {
			var args struct {
				Path string `json:"path"`
			}
			path := ""
			if json.Unmarshal(call.Arguments, &args) == nil {
				path = slashed(args.Path)
			}
			calls = append(calls, actingCall{id: call.ID, tool: call.Name, path: path})
		}
		if message.Role == llm.RoleTool && message.ToolCallID != "" {
			results[message.ToolCallID] = message.Content
		}
	}
	return calls, results
}

func SearchLinksOf(conversation []llm.Message) []SearchLink {
	calls, results := callsInOrder(conversation)
	var links []SearchLink
	for i, call := range calls {
		if call.tool != "search" {
			continue
		}
		offered := search.OfferedPaths(results[call.id])
		link := SearchLink{SearchCallID: call.id, Offered: offered, Kind: SearchLinkNone}
		for _, later := range calls[i+1:] {
			if !actsOnAPath(later.tool) || later.path == "" {
				continue
			}
			link.ActedCallID, link.ActedPath, link.Kind = later.id, later.path, SearchLinkUnoffered
			for _, candidate := range offered {
				if slashed(candidate) == later.path {
					link.Kind = SearchLinkHit
					break
				}
			}
			break
		}
		links = append(links, link)
	}
	return links
}
