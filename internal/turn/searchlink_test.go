package turn

import (
	"encoding/json"
	"reflect"
	"testing"

	"tofu/internal/llm"
)

func searchResultText(paths ...string) string {
	text := ""
	for _, path := range paths {
		text += "\n" + path + ":1-2 lines, matched on line 1 in code\nbody\n"
	}
	return text
}

func searchCall(id, path string) llm.Message {
	args, _ := json.Marshal(map[string]string{"pattern": "x", "path": path})
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "search", Arguments: args}}}
}

func searchResult(id string, paths ...string) llm.Message {
	return llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: searchResultText(paths...)}
}

func actCall(id, tool, path string) llm.Message {
	args, _ := json.Marshal(map[string]string{"path": path})
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: tool, Arguments: args}}}
}

func actResult(id string) llm.Message {
	return llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: "done"}
}

func TestSearchLinksNamesTheOfferedPathALaterEditActedIn(t *testing.T) {
	conversation := []llm.Message{
		searchCall("s1", "."),
		searchResult("s1", "a.go", "b.go", "c.go"),
		actCall("e1", "edit", "b.go"),
		actResult("e1"),
	}
	links := searchLinksOf(conversation)
	want := []SearchLink{{SearchCallID: "s1", Offered: []string{"a.go", "b.go", "c.go"}, ActedCallID: "e1", ActedPath: "b.go", Kind: SearchLinkHit}}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("searchLinksOf = %+v, want %+v", links, want)
	}
}

func TestSearchLinksRecordsNoneWhenNothingActsOnAPathAfterwards(t *testing.T) {
	conversation := []llm.Message{
		searchCall("s1", "."),
		searchResult("s1", "a.go", "b.go"),
	}
	links := searchLinksOf(conversation)
	want := []SearchLink{{SearchCallID: "s1", Offered: []string{"a.go", "b.go"}, Kind: SearchLinkNone}}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("searchLinksOf = %+v, want %+v", links, want)
	}
}

func TestSearchLinksRecordsUnofferedWhenTheActedPathWasNeverReturned(t *testing.T) {
	conversation := []llm.Message{
		searchCall("s1", "."),
		searchResult("s1", "a.go", "b.go"),
		actCall("w1", "write", "z.go"),
		actResult("w1"),
	}
	links := searchLinksOf(conversation)
	want := []SearchLink{{SearchCallID: "s1", Offered: []string{"a.go", "b.go"}, ActedCallID: "w1", ActedPath: "z.go", Kind: SearchLinkUnoffered}}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("searchLinksOf = %+v, want %+v", links, want)
	}
	if links[0].Kind == SearchLinkHit {
		t.Fatalf("a path the search never returned must not read as a hit")
	}
}
