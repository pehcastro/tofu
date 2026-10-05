package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"tofu/internal/session"
)

const (
	lookbackBlocks          = 20
	assistantBlocks         = 2
	readRate                = 0.1
	fiveMinuteWriteRate     = 1.25
	oneHourWriteRate        = 2.0
	outputRate              = 5.0
	opusDollarsPerMillion   = 4.0
	sonnetDollarsPerMillion = 2.0
	neverSeen               = time.Duration(1 << 62)
	humanPause              = 30 * time.Minute
)

type requestBody struct {
	Model    string `json:"model"`
	Input    int    `json:"prompt_tokens"`
	Output   int    `json:"completion_tokens"`
	Read     int    `json:"cache_read_tokens"`
	Write    int    `json:"cache_write_tokens"`
	Duration int64  `json:"duration_ms"`
}

type row struct {
	requestBody
	agent, role string
	start       time.Time
	blocks      int
	lead        bool
}

type lifetime struct {
	ttl       time.Duration
	writeRate float64
}

type layout struct {
	name          string
	head, history lifetime
}

type gapShare struct{ gaps, overFive, overHour int }

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	projects := flag.String("projects", filepath.Join(home, ".tofu", "projects"), "the recorded sessions, read only")
	project := flag.String("project", "*", "a glob over the project directory names")
	flag.Parse()
	paths, err := filepath.Glob(filepath.Join(*projects, *project, "sessions", "*", "events.jsonl"))
	if err != nil {
		return err
	}
	var rows []row
	sessions, undated := 0, 0
	for _, path := range paths {
		read, missing, err := claudeRows(path)
		if err != nil {
			return err
		}
		if len(read) > 0 {
			sessions++
		}
		rows, undated = append(rows, read...), undated+missing
	}
	slices.SortStableFunc(rows, func(a, b row) int { return a.start.Compare(b.start) })

	hour, five := lifetime{time.Hour, oneHourWriteRate}, lifetime{5 * time.Minute, fiveMinuteWriteRate}
	layouts := []layout{
		{"all 1 hour (today)", hour, hour},
		{"head 1 hour, history 5 minutes", hour, five},
		{"all 5 minutes", five, five},
	}
	cacheCost, pauseCost := make([]float64, len(layouts)), make([]float64, len(layouts))
	var plainCost float64
	shares := map[bool]*gapShare{true: {}, false: {}}
	lastByAgent, lastByRole, head := map[string]time.Time{}, map[string]time.Time{}, map[string]int{}
	wide, wideMissed, cached := 0, 0, 0
	for _, r := range rows {
		agentGap, headGap := neverSeen, neverSeen
		if last, ok := lastByAgent[r.agent]; ok {
			agentGap = r.start.Sub(last)
			share := shares[r.lead]
			share.gaps++
			if agentGap > five.ttl {
				share.overFive++
			}
			if agentGap > hour.ttl {
				share.overHour++
			}
		} else {
			head[r.agent] = r.Read + r.Write
		}
		if last, ok := lastByRole[r.role]; ok {
			headGap = r.start.Sub(last)
		}
		lastByAgent[r.agent], lastByRole[r.role] = r.start, r.start
		if r.Read+r.Write > 0 {
			cached++
		}

		headTokens := min(head[r.agent], r.Read+r.Write)
		headRead := min(r.Read, headTokens)
		headWrite, historyRead := headTokens-headRead, r.Read-headRead
		historyWrite := r.Write - headWrite
		if r.blocks > lookbackBlocks {
			wide++
			if historyRead == 0 && historyWrite > 0 {
				wideMissed++
			}
		}
		base := opusDollarsPerMillion
		if strings.Contains(r.Model, "sonnet") {
			base = sonnetDollarsPerMillion
		}
		plainCost += base * (float64(r.Input) + outputRate*float64(r.Output)) / 1e6
		for i, l := range layouts {
			cost := base * (l.head.price(headRead, headWrite, headGap) + l.history.price(historyRead, historyWrite, agentGap)) / 1e6
			cacheCost[i] += cost
			if r.lead && agentGap != neverSeen {
				pauseCost[i] += base*(l.head.price(headRead, headWrite, humanPause)+l.history.price(historyRead, historyWrite, humanPause))/1e6 - cost
			}
		}
	}

	fmt.Printf("cache lifetime replay, %s, offline over %s\n", time.Now().Format("2006-01-02 15:04"), *projects)
	fmt.Printf("session files %d, with an Anthropic request %d, Anthropic requests %d, with cache figures %d, left out for no duration %d\n",
		len(paths), sessions, len(rows), cached, undated)
	for _, lead := range []bool{true, false} {
		s := shares[lead]
		fmt.Printf("%-10s gaps %6d, over 5 minutes %5d (%.2f%%), over 1 hour %4d (%.2f%%)\n",
			map[bool]string{true: "lead", false: "sub-agent"}[lead], s.gaps, s.overFive, percent(s.overFive, s.gaps), s.overHour, percent(s.overHour, s.gaps))
	}
	fmt.Printf("requests adding over %d blocks since the previous breakpoint %d, of them with no history read %d (the same under every layout)\n",
		lookbackBlocks, wide, wideMissed)
	fmt.Printf("list price: Opus $%.2f, Sonnet $%.2f a million input; read %.2fx, 5-minute write %.2fx, 1-hour write %.2fx, output %.0fx\n\n",
		opusDollarsPerMillion, sonnetDollarsPerMillion, readRate, fiveMinuteWriteRate, oneHourWriteRate, outputRate)
	fmt.Printf("a pause is a lead gap of %s, the person reading between prompts; none is in the record, so its cost is priced over every lead request\n\n", humanPause)
	fmt.Printf("%-32s %12s %12s %10s %16s %16s\n", "layout", "cache USD", "total USD", "vs today", "USD a pause", "pauses to undo")
	leadGaps := float64(max(shares[true].gaps, 1))
	for i, l := range layouts {
		undo := "-"
		if i > 0 {
			undo = fmt.Sprintf("%.0f", (cacheCost[0]-cacheCost[i])/((pauseCost[i]-pauseCost[0])/leadGaps))
		}
		fmt.Printf("%-32s %12.2f %12.2f %+9.2f%% %16.4f %16s\n", l.name, cacheCost[i], cacheCost[i]+plainCost,
			100*(cacheCost[i]-cacheCost[0])/(cacheCost[0]+plainCost), pauseCost[i]/leadGaps, undo)
	}
	return nil
}

func (l lifetime) price(read, write int, gap time.Duration) float64 {
	if gap > l.ttl {
		return float64(read+write) * l.writeRate
	}
	return float64(read)*readRate + float64(write)*l.writeRate
}

func percent(part, whole int) float64 {
	return 100 * float64(part) / float64(max(whole, 1))
}

func claudeRows(path string) ([]row, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	sessionDir := filepath.Dir(path)
	project := filepath.Base(filepath.Dir(filepath.Dir(sessionDir)))
	serial := regexp.MustCompile(`-\d+$`)
	blocks := map[string]int{}
	var rows []row
	undated := 0
	for line := range bytes.Lines(raw) {
		var event session.Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", path, err)
		}
		switch event.Kind {
		case session.EventMessage, session.EventToolCall, session.EventToolResult:
			blocks[event.Agent]++
		case session.EventRequest:
			var body requestBody
			if err := json.Unmarshal(event.Body, &body); err != nil {
				return nil, 0, fmt.Errorf("%s: %w", path, err)
			}
			added := blocks[event.Agent] + assistantBlocks
			blocks[event.Agent] = 0
			if !strings.HasPrefix(body.Model, "claude-") {
				continue
			}
			if body.Duration <= 0 {
				undated++
				continue
			}
			rows = append(rows, row{
				requestBody: body,
				agent:       sessionDir + "|" + event.Agent,
				role:        project + "|" + serial.ReplaceAllString(event.Agent, ""),
				start:       event.At.Add(-time.Duration(body.Duration) * time.Millisecond),
				blocks:      added,
				lead:        event.Agent == "",
			})
		}
	}
	return rows, undated, nil
}
