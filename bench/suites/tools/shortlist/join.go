package shortlist

import (
	"regexp"
	"strings"

	"tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
	"tofu/internal/store/session"
	"tofu/internal/tools/code/search"
)

func slashed(path string) string {
	return strings.ReplaceAll(path, `\`, "/")
}

var unitHeader = regexp.MustCompile(`(?m)^(\S+):\d+-\d+ [^\n]*\n`)

func unitBodies(rendered string) map[string]string {
	locations := unitHeader.FindAllStringSubmatchIndex(rendered, -1)
	bodies := make(map[string]string, len(locations))
	for i, loc := range locations {
		path := slashed(rendered[loc[2]:loc[3]])
		start := loc[1]
		end := len(rendered)
		if i+1 < len(locations) {
			end = locations[i+1][0]
		}
		if _, seen := bodies[path]; seen {
			continue
		}
		bodies[path] = strings.TrimSpace(rendered[start:end])
	}
	return bodies
}

type JoinCounts struct {
	Sessions  int
	Searches  int
	Hits      int
	Unoffered int
	None      int
	Distinct  int
}

func (c JoinCounts) UnofferedRate() float64 {
	actedOn := c.Hits + c.Unoffered
	if actedOn == 0 {
		return 0
	}
	return float64(c.Unoffered) / float64(actedOn)
}

func rowFromHit(turnID, task string, link search.SearchLink, results map[string]string) Row {
	bodies := unitBodies(results[link.SearchCallID])
	files := make([]File, 0, len(link.Offered))
	for _, offered := range link.Offered {
		path := slashed(offered)
		content := bodies[path]
		if path == link.ActedPath {
			if full, ok := results[link.ActedCallID]; ok && strings.TrimSpace(full) != "" {
				content = full
			}
		}
		files = append(files, File{Path: path, Content: content})
	}
	return Row{TurnID: turnID, Task: task, Files: files, Label: []string{link.ActedPath}}
}

func BuildJoinCorpus(sessionsDir string) ([]Row, JoinCounts, error) {
	store := session.NewStore(sessionsDir)
	listing, err := store.Listing()
	if err != nil {
		return nil, JoinCounts{}, err
	}
	var counts JoinCounts
	var rows []Row
	tasksSeen := make(map[string]bool)
	for _, header := range listing.Sessions {
		counts.Sessions++
		events, err := store.Body(header.ID)
		if err != nil {
			return nil, JoinCounts{}, err
		}
		conversation, err := loop.ConversationFrom(events)
		if err != nil {
			return nil, JoinCounts{}, err
		}
		results := make(map[string]string)
		for _, message := range conversation {
			if message.Role == core.RoleTool && message.ToolCallID != "" {
				results[message.ToolCallID] = message.Content
			}
		}
		for _, link := range search.SearchLinksOf(conversation) {
			counts.Searches++
			switch link.Kind {
			case search.SearchLinkHit:
				counts.Hits++
			case search.SearchLinkUnoffered:
				counts.Unoffered++
			case search.SearchLinkNone:
				counts.None++
			}
			if link.Kind != search.SearchLinkHit {
				continue
			}
			task := strings.TrimSpace(header.Task)
			if task == "" || tasksSeen[task] {
				continue
			}
			tasksSeen[task] = true
			counts.Distinct++
			rows = append(rows, rowFromHit(header.ID+"#"+link.SearchCallID, task, link, results))
		}
	}
	return rows, counts, nil
}
