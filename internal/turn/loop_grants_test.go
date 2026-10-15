package turn

import (
	"fmt"
	"testing"

	"tofu/internal/llm"
)

func TestASubAgentWhoseGrantsShrinkMidRunRecordsEachNewAskAndRunsOn(t *testing.T) {
	const writer = "write other/x.txt and then far/y.txt"
	model := newCrew(map[string][]llm.Decision{
		leadKey: {
			spawnCall("call-a", writer, "mine/**"),
			messageDoing("grant", "sub-1", "grant", "other/**"),
			claimDecision("granted other/** to sub-1"),
			claimDecision("read sub-1's report"),
			claimDecision("read it again"),
		},
		writer: {
			writeCall("first", "other/x.txt"),
			writeCall("again", "other/x.txt"),
			writeCall("far", "far/y.txt"),
			claimDecision("wrote other/x.txt, far/y.txt is outside my paths"),
		},
	})
	releaseWriter := model.hold(writer, 2)
	defer releaseWriter()
	lead, _ := writingCrew(t, model)
	typed := make(chan string, 1)
	led := startLead(t.Context(), lead, typed)
	waitFor(t, "sub-1 asking for its second step", func() bool { return len(model.requests(writer)) >= 2 })
	typed <- "grant sub-1 other/**"
	waitFor(t, "the lead's grant to come back", func() bool { return len(model.requests(leadKey)) >= 3 })
	releaseWriter()
	led.wait(t)

	var grants [][]string
	for _, row := range lead.Tools.byName["spawn"].(*SpawnTool).SubAgentRows() {
		for _, step := range row.Steps {
			var asked []string
			for _, question := range step.Grants {
				asked = append(asked, question.Where)
			}
			grants = append(grants, asked)
			var calls []string
			for _, call := range step.ToolCalls {
				calls = append(calls, call.Tool+" "+string(call.Args)+" error="+call.Error)
			}
			t.Logf("step %d calls %q grants %q", len(grants), calls, asked)
		}
	}
	if shown := fmt.Sprint(grants); shown != "[[other/x.txt] [] [far/y.txt] []]" {
		t.Errorf("sub-1's step grants are %s, want [[other/x.txt] [] [far/y.txt] []]", shown)
	}
}
