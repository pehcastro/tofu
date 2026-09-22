package anthropic

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tofu/internal/llm"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestTheCommittedBreakpointMovesForwardAsTheConversationGrows(t *testing.T) {
	var anchors, tips []int
	for exchanges := 2; exchanges <= 4; exchanges++ {
		indexes := messageBreakpoints(t, historyRequest(exchanges))
		if len(indexes) != 2 {
			t.Fatalf("%d exchanges broke the cache at %v, want an anchor and a tip", exchanges, indexes)
		}
		anchors = append(anchors, indexes[0])
		tips = append(tips, indexes[1])
	}
	for request := 1; request < len(anchors); request++ {
		if anchors[request] <= anchors[request-1] {
			t.Fatalf("the anchor sat at %v over three successive requests and never moved forward", anchors)
		}
		if tips[request] <= tips[request-1] {
			t.Fatalf("the tip sat at %v over three successive requests", tips)
		}
		if anchors[request] != tips[request]-2 {
			t.Fatalf("request %d anchored at %d with its tip at %d, want the anchor on the last committed message",
				request, anchors[request], tips[request])
		}
	}
}

func TestACommittedPrefixUnderTheMinimumGetsNoSecondBreakpoint(t *testing.T) {
	request := minimalRequest()
	request.Messages = append(request.Messages, exchange(1, "a short first result")...)
	request.Messages = append(request.Messages, exchange(2, strings.Repeat("x", historyCacheMinPrefixChars))...)

	indexes := messageBreakpoints(t, request)
	if len(indexes) != 1 || indexes[0] != len(request.Messages)-1 {
		t.Fatalf("a committed prefix under %d characters was anchored at %v",
			historyCacheCommittedMinChars, indexes)
	}
}

func TestALongConversationStaysInsideTheBreakpointAllowance(t *testing.T) {
	request := historyRequest(40)
	request.System = []string{"the project instructions"}
	request.Tools = []llm.Tool{{Name: "read"}, {Name: "write"}}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	if count := strings.Count(string(body), `"cache_control"`); count > cacheBreakpointsPerRequest {
		t.Fatalf("a 40 exchange conversation carries %d breakpoints, past the %d the vendor allows",
			count, cacheBreakpointsPerRequest)
	}
}

func TestTheEncodedRequestPlacesItsMarkersWhereTheGoldenSays(t *testing.T) {
	request := minimalRequest()
	for step := 1; step <= 3; step++ {
		request.Messages = append(request.Messages, exchange(step, strings.Repeat("read\n", 420))...)
	}
	body, err := request.Encode(true)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("indenting: %v", err)
	}

	path := filepath.Join("testdata", "history-caching.golden")
	if *update {
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, indented.Bytes()) {
		t.Errorf("the encoded request does not match %s\n--- got ---\n%s", path, indented.String())
	}
}
