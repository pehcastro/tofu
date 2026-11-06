package trail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/tools/contract"
	"tofu/internal/tools/history/shadow"
)

func (t Trail) ownerOf(ctx context.Context, turns []shadow.Recorded, line string) (string, error) {
	colon := strings.LastIndex(line, ":")
	file, number := line[:max(colon, 0)], line[colon+1:]
	at, err := strconv.Atoi(number)
	if colon < 1 || err != nil || at < 1 {
		return "", fmt.Errorf("line %q is not path:number, for instance order.go:15", line)
	}
	file = filepath.ToSlash(filepath.Clean(file))
	revs, err := os.CreateTemp("", "tofu-trail-revs-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(revs.Name()) }()
	var chain strings.Builder
	for i := len(turns) - 1; i >= 0; i-- {
		fmt.Fprintf(&chain, "%s %s\n", turns[i].End, turns[i].Start)
		if i > 0 {
			fmt.Fprintf(&chain, "%s %s\n", turns[i].Start, turns[i-1].End)
		}
	}
	fmt.Fprintf(&chain, "%s\n", turns[0].Start)
	_, err = revs.WriteString(chain.String())
	if closeErr := revs.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	blamed, err := t.Store.Git(ctx, "", "blame", "--porcelain", "-L", number+","+number, "-S", revs.Name(), "--contents", filepath.Join(t.Store.Work(), filepath.FromSlash(file)), turns[len(turns)-1].End, "--", file)
	if err != nil {
		return "", err
	}
	commit, _, _ := strings.Cut(blamed, " ")
	text := blamed[strings.LastIndex(blamed, "\n\t")+len("\n\t"):]
	head := fmt.Sprintf("%s:%d %q\n", file, at, strings.TrimRight(text, "\r\n"))
	wrote := slices.IndexFunc(turns, func(one shadow.Recorded) bool { return one.End == commit })
	if wrote < 0 {
		before := slices.IndexFunc(turns, func(one shadow.Recorded) bool { return one.Start == commit })
		switch {
		case commit == shadow.AbsentBlob:
			return head + "changed since the last recorded turn ended, outside any recorded turn\n", nil
		case before == 0:
			return head + "older than the oldest turn the shadow store keeps, " + turns[0].ID + "\n", nil
		case before > 0:
			return head + "changed outside any recorded turn, after turn " + turns[before-1].ID + " ended and before turn " + turns[before].ID + " started\n", nil
		}
		return "", fmt.Errorf("git blame named commit %s, which is no turn the shadow store at %s records", commit, t.Store.Dir())
	}
	one := turns[wrote]
	said, err := spoken(t.Sessions, []shadow.Recorded{one})
	if err != nil {
		return "", err
	}
	var calls []string
	if which := slices.IndexFunc(one.Changes, func(c shadow.Change) bool { return c.Path == file }); which >= 0 {
		calls = one.Changes[which].By
	}
	out := fmt.Sprintf("%swritten by session %s, turn %s, agent %s, calls %s, message %s\n", head, t.handle(one.Session), one.ID, one.Agent, contract.Listed(calls), task(said, one.ID))
	for _, inner := range turns[:wrote] {
		if inner.ID > one.ID && slices.ContainsFunc(inner.Changes, func(c shadow.Change) bool { return c.Path == file }) {
			out += "inside it, turn " + inner.ID + ", agent " + inner.Agent + ", also changed " + file + "\n"
		}
	}
	return out, nil
}
