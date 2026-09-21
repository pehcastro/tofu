package promote

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/bench/corpus"
	"tofu/internal/session"
)

const (
	RowsToDecideAnything = 78
	labelColumn          = 40
)

func Path(stateDir string) string {
	return filepath.Join(stateDir, session.PromotionsFileName)
}

func Read(stateDir string) ([]session.Promotion, error) {
	path := Path(stateDir)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var rows []session.Promotion
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		row, err := parse(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("%s: row %d: %w", path, len(rows)+1, err)
		}
		rows = append(rows, row)
	}
	return rows, scanner.Err()
}

func parse(line string) (session.Promotion, error) {
	var row session.Promotion
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		return row, err
	}
	switch row.Action {
	case session.ReachedIntoWork, session.MovedKind:
	default:
		return row, fmt.Errorf("%q is not an action the interface records", row.Action)
	}
	for _, place := range []session.Place{row.FreeArm, row.Chose} {
		switch place {
		case session.PlaceChat, session.PlaceWork:
		default:
			return row, fmt.Errorf("%q is not a place a block can be", place)
		}
	}
	if row.EventKind == "" {
		return row, errors.New("the row names no event kind")
	}
	if leaks := corpus.LeaksIn(line); len(leaks) > 0 {
		return row, fmt.Errorf("carries %q, which came off the recording machine", leaks)
	}
	return row, nil
}

func Report(rows []session.Promotion, stateDir string) string {
	counted := map[session.PromotionAction]int{}
	kinds := map[string]int{}
	disagreed := 0
	for _, row := range rows {
		counted[row.Action]++
		kinds[row.EventKind]++
		if row.FreeArm != row.Chose {
			disagreed++
		}
	}
	lines := []string{
		"promote corpus at " + Path(stateDir),
		line("rows", len(rows)),
		line("reached into work", counted[session.ReachedIntoWork]),
		line("moved a kind in settings", counted[session.MovedKind]),
		line("the free arm was already right", len(rows)-disagreed),
		line("the free arm was wrong", disagreed),
		line("rows still needed to decide anything", max(RowsToDecideAnything-len(rows), 0)),
	}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		lines = append(lines, line("kind "+kind, kinds[kind]))
	}
	return strings.Join(lines, "\n")
}

func line(label string, count int) string {
	return label + strings.Repeat(" ", max(labelColumn-len(label), 1)) + strconv.Itoa(count)
}
