package subagent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"tofu/internal/konst"
)

type Claim struct {
	Line    string `json:"line"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Cut     bool   `json:"cut,omitempty"`
	Met     bool   `json:"met"`
}

func (c Claim) Omitted() bool {
	return strings.TrimSpace(c.Command) == "" && strings.TrimSpace(c.Output) == ""
}

type Contract struct {
	TicketID string   `json:"ticket_id,omitempty"`
	NoTicket bool     `json:"no_ticket,omitempty"`
	Partial  bool     `json:"partial,omitempty"`
	Claims   []Claim  `json:"claims,omitempty"`
	Wrote    []string `json:"wrote,omitempty"`
	Blocked  []string `json:"blocked,omitempty"`
}

func (c Contract) Omissions() int {
	if c.Partial {
		return 0
	}
	omitted := 0
	for _, claim := range c.Claims {
		if claim.Omitted() {
			omitted++
		}
	}
	return omitted
}

func (c Contract) Block() string {
	if c.NoTicket {
		return "contract: no ticket, so no acceptance lines and no contract"
	}
	summary := fmt.Sprintf("contract %s: %d of %d acceptance lines omitted", c.TicketID, c.Omissions(), len(c.Claims))
	if c.Partial {
		summary += " (partial: the sub-agent was stopped before finishing, so unaddressed lines are not counted as omissions)"
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return summary + "\ncontract could not be printed: " + err.Error()
	}
	return summary + "\n" + string(data)
}

var (
	ticketPattern    = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,15}-\d+\b`)
	reportedContract = regexp.MustCompile("(?s)```json\\s*(\\{.*?\\})\\s*```")
)

func TicketID(task string) string {
	return ticketPattern.FindString(task)
}

func AcceptanceLines(task string) []string {
	lines := strings.Split(task, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## Acceptance" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	return acceptanceBullets(lines[start:])
}

func acceptanceBullets(lines []string) []string {
	var found []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			found = append(found, strings.TrimSpace(current.String()))
			current.Reset()
		}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "## "):
			flush()
			return found
		case strings.HasPrefix(line, "- "):
			flush()
			current.WriteString(strings.TrimPrefix(line, "- "))
		case current.Len() > 0 && strings.HasPrefix(line, "  ") && trimmed != "":
			current.WriteString(" ")
			current.WriteString(trimmed)
		default:
			flush()
		}
	}
	flush()
	return found
}

type reportedClaim struct {
	Line    string `json:"line"`
	Command string `json:"command"`
	Output  string `json:"output"`
	Met     bool   `json:"met"`
}

type reported struct {
	Claims  []reportedClaim `json:"claims"`
	Blocked []string        `json:"blocked"`
}

func parseReported(text string) (reported, bool) {
	matches := reportedContract.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return reported{}, false
	}
	var parsed reported
	if err := json.Unmarshal([]byte(matches[len(matches)-1][1]), &parsed); err != nil {
		return reported{}, false
	}
	return parsed, true
}

func capped(text string) (string, bool) {
	if len(text) <= konst.TurnResultBytesCap {
		return text, false
	}
	return text[:konst.TurnResultBytesCap], true
}

func BuildContract(task, report string, stopped bool) Contract {
	return contractOf(TicketID(task), AcceptanceLines(task), report, stopped)
}

func contractOf(ticket string, lines []string, report string, stopped bool) Contract {
	if ticket == "" || len(lines) == 0 {
		return Contract{NoTicket: true}
	}
	given, found := parseReported(report)
	byLine := make(map[string]reportedClaim, len(given.Claims))
	for _, claim := range given.Claims {
		byLine[strings.TrimSpace(claim.Line)] = claim
	}
	contract := Contract{TicketID: ticket, Partial: stopped, Blocked: given.Blocked}
	for _, line := range lines {
		claim := Claim{Line: line}
		if match, ok := byLine[strings.TrimSpace(line)]; found && ok {
			claim.Command = match.Command
			claim.Output, claim.Cut = capped(match.Output)
			claim.Met = match.Met
		}
		contract.Claims = append(contract.Claims, claim)
	}
	return contract
}
