package crew

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Ticket struct {
	ID     string
	Status string
	Owns   []string
}

type OverlapPair struct {
	A, B string
}

var ownsHeaderExpr = regexp.MustCompile(`^owns:\s*$`)
var ownsLineExpr = regexp.MustCompile(`^\s*-\s*(.+?)\s*$`)

func ParseOwns(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var owns []string
	inFront, inOwns := false, false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFront {
				inFront = true
				continue
			}
			break
		}
		if !inFront {
			continue
		}
		if ownsHeaderExpr.MatchString(line) {
			inOwns = true
			continue
		}
		if !inOwns {
			continue
		}
		if m := ownsLineExpr.FindStringSubmatch(line); m != nil {
			owns = append(owns, m[1])
			continue
		}
		if len(line) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inOwns = false
		}
	}
	return owns, scanner.Err()
}

var ticketFolders = []string{"open", "doing", "review", "done", "needs-decision", "dropped"}

func ReadBoard(repoRoot string) ([]Ticket, error) {
	base := filepath.Join(repoRoot, ".local", "boji", "tickets")
	var tickets []Ticket
	for _, folder := range ticketFolders {
		dir := filepath.Join(base, folder)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			owns, err := ParseOwns(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			tickets = append(tickets, Ticket{
				ID:     strings.TrimSuffix(entry.Name(), ".md"),
				Status: folder,
				Owns:   owns,
			})
		}
	}
	return tickets, nil
}

func AuditDoing(tickets []Ticket) ([]OverlapPair, error) {
	var doing []Ticket
	for _, t := range tickets {
		if t.Status == "doing" {
			doing = append(doing, t)
		}
	}
	var pairs []OverlapPair
	for i := 0; i < len(doing); i++ {
		for j := i + 1; j < len(doing); j++ {
			ok, err := Overlap(doing[i].Owns, doing[j].Owns)
			if err != nil {
				return nil, err
			}
			if ok {
				pairs = append(pairs, OverlapPair{A: doing[i].ID, B: doing[j].ID})
			}
		}
	}
	return pairs, nil
}
