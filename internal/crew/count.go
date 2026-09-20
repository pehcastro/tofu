package crew

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Tally struct {
	Widenings []string
	Deferred  []string
	Grants    []string
}

var (
	selfWidening   = regexp.MustCompile(`(?i)(\bi added\b|by widening)[^.]{0,80}owns`)
	deferredClause = regexp.MustCompile(`open question|not by me`)
)

func logBlocks(ticket string) []string {
	body := strings.ReplaceAll(ticket, "\r\n", "\n")
	start := strings.Index(body, "\n## Log")
	if start < 0 {
		return nil
	}
	return strings.Split(body[start:], "\n### ")[1:]
}

func flowed(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func blockedParagraph(block string) string {
	for _, paragraph := range strings.Split(block, "\n\n") {
		if strings.HasPrefix(strings.TrimSpace(paragraph), "BLOCKED") {
			return flowed(paragraph)
		}
	}
	return ""
}

func tallyBlock(ticketID, block string, tally *Tally) {
	if question, err := ParseBlock("### " + block); err == nil {
		switch question.Kind {
		case Grant:
			tally.Grants = append(tally.Grants, ticketID)
		case Deferred:
			tally.Deferred = append(tally.Deferred, ticketID)
		case Blocking, Assumption:
		}
		return
	}
	if selfWidening.MatchString(flowed(block)) {
		tally.Widenings = append(tally.Widenings, ticketID)
	}
	declared := strings.ToLower(blockedParagraph(block))
	if deferredClause.MatchString(declared) && !strings.Contains(declared, "no open question") {
		tally.Deferred = append(tally.Deferred, ticketID)
	}
}

func Count(dir string) (Tally, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return Tally{}, err
	}
	var tally Tally
	for _, file := range files {
		ticket, err := os.ReadFile(file)
		if err != nil {
			return Tally{}, err
		}
		ticketID := strings.TrimSuffix(filepath.Base(file), ".md")
		for _, block := range logBlocks(string(ticket)) {
			tallyBlock(ticketID, block, &tally)
		}
	}
	return tally, nil
}
