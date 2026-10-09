package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tofu/internal/memtree"
	"tofu/internal/sys"
)

const (
	EpisodesStore = "episodes"
	episodesDir   = "episodes"
	episodesHead  = "the conversation in this project so far, oldest first. A line is id+n|kind: text, and an older line sums up n items: user is the person, lead is you, work is a sub-agent's report. zoom opens a line, recall searches the words:\n"
	leftOut       = "(the %d oldest lines are left out to fit this context; recall finds their words)\n"
)

func episodesLog(project string) (string, error) {
	full, err := filepath.Abs(project)
	if err != nil {
		return "", err
	}
	state, err := sys.ProjectStateDirAt(full)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(state, episodesDir)
	return filepath.Join(dir, logFile), os.MkdirAll(dir, 0o755)
}

func openEpisodes(project string) (*memtree.Store, error) {
	log, err := episodesLog(project)
	if err != nil {
		return nil, err
	}
	return memtree.Open(log)
}

func KeepEpisode(project string, item memtree.Item) error {
	store, err := openEpisodes(project)
	if err != nil {
		return err
	}
	return errors.Join(store.Append(item), store.Close())
}

func EpisodeView(project string, budget int, compact memtree.Compact) (string, memtree.Built, error) {
	store, err := openEpisodes(project)
	if err != nil {
		return "", memtree.Built{}, err
	}
	if compact == nil {
		compact = func(context.Context, string, string, string) (string, error) {
			return "", errors.New("an episode summary is written between turns, never while the view is read")
		}
	}
	built, err := store.Build(context.Background(), compact)
	if err == nil {
		err = store.Advance(budget, budget/2)
	}
	return fitted(store.View(), budget), built, errors.Join(err, store.Close())
}

func fitted(view string, budget int) string {
	lines := strings.SplitAfter(strings.TrimSuffix(view, "\n"), "\n")
	kept := lines
	for len(kept) > 0 && len(episodesHead)+len(leftOut)+len(strings.Join(kept, "")) > budget {
		kept = kept[1:]
	}
	switch {
	case view == "" || len(kept) == 0:
		return ""
	case len(kept) < len(lines):
		return episodesHead + fmt.Sprintf(leftOut, len(lines)-len(kept)) + strings.Join(kept, "") + "\n"
	}
	return episodesHead + view
}

func Trees(project string) (map[string]string, error) {
	logs := map[string]string{}
	episodes, err := episodesLog(project)
	if err != nil {
		return nil, err
	}
	logs[EpisodesStore] = episodes
	m, err := Open(project)
	if err != nil {
		return nil, err
	}
	mine, err := m.author(m.repo, false)
	if err != nil {
		return nil, err
	}
	for _, shelf := range m.all() {
		for _, store := range shelf.stores {
			log := filepath.Join(m.treeDir(store), logFile)
			if _, missing := os.Stat(log); missing != nil || shelf.Scope == UserLocal && filepath.Base(store) != mine {
				continue
			}
			logs[string(shelf.Scope)] = log
		}
	}
	return logs, nil
}
