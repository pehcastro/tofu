package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"tofu/internal/session"
	"tofu/internal/sys"
)

type corpusSession struct {
	ID       string
	At       time.Time
	Shape    session.Shape
	Steps    int
	Messages int
	Reads    int
	Unknown  []session.EventKind
}

type corpus struct {
	Dir        string
	Sessions   []corpusSession
	Unreadable []string
}

func benchCorpus(out, errOut io.Writer, args []string) int {
	dir := ""
	for i := 0; i < len(args); i++ {
		if args[i] != "--dir" {
			return fail(errOut, "corpus", fmt.Errorf("unknown argument %q, the only one is --dir", args[i]))
		}
		named, err := nextArg(args, &i, "--dir")
		if err != nil {
			return fail(errOut, "corpus", err)
		}
		dir = named
	}
	if dir == "" {
		state, err := sys.ProjectStateDir()
		if err != nil {
			return fail(errOut, "corpus", err)
		}
		dir = filepath.Join(state, "sessions")
	}
	read, err := readCorpus(dir)
	if err != nil {
		return fail(errOut, "corpus", err)
	}
	_, _ = fmt.Fprint(out, renderCorpus(read))
	return exitOK
}

func readCorpus(dir string) (corpus, error) {
	store := session.NewStore(dir)
	listing, err := store.Listing()
	if err != nil {
		return corpus{}, err
	}
	read := corpus{Dir: dir}
	for _, skip := range listing.Skipped {
		read.Unreadable = append(read.Unreadable, fmt.Sprintf("%s: %v", skip.ID, skip.Reason))
	}
	for _, header := range listing.Sessions {
		reading, err := store.Reading(header.ID)
		if err != nil {
			read.Unreadable = append(read.Unreadable, fmt.Sprintf("%s: %v", header.ID, err))
			continue
		}
		read.Sessions = append(read.Sessions, corpusSession{
			ID:       header.ID,
			At:       header.At,
			Shape:    store.Shape(header.ID),
			Steps:    len(reading.Steps),
			Messages: len(reading.Messages),
			Reads:    len(reading.Reads),
			Unknown:  reading.Unknown,
		})
	}
	return read, nil
}

func renderCorpus(read corpus) string {
	body := &strings.Builder{}
	fmt.Fprintf(body, "bench corpus: %s\nno model is called and no wire is opened by this verb\n\n", read.Dir)

	steps, messages, reads, eventFiles := 0, 0, 0, 0
	var thinned []string
	for _, one := range read.Sessions {
		steps, messages, reads = steps+one.Steps, messages+one.Messages, reads+one.Reads
		if one.Shape == session.ShapeEvents {
			eventFiles++
		}
		verdict := "every kind read"
		if len(one.Unknown) > 0 {
			unread := kindList(one.Unknown)
			verdict = "not read: " + unread
			thinned = append(thinned, fmt.Sprintf("%s in %s", unread, one.ID))
		}
		fmt.Fprintf(body, "%s  %s  %s  steps %d  messages %d  reads %d  %s\n",
			one.ID, one.At.Format(time.RFC3339), one.Shape, one.Steps, one.Messages, one.Reads, verdict)
	}

	fmt.Fprintf(body, "\ntotals: sessions %d, steps %d, message events %d, read events %d\n",
		len(read.Sessions), steps, messages, reads)
	fmt.Fprintf(body, "recorded as %s: %d, recorded as a %s: %d\n",
		session.ShapeEvents, eventFiles, session.ShapeSingleFile, len(read.Sessions)-eventFiles)
	fmt.Fprintf(body, "this build reads these kinds: %s\n", kindList(session.Kinds()))
	if len(thinned) == 0 {
		fmt.Fprint(body, "no session here carries a kind this build does not read, so nothing was skipped\n")
	} else {
		fmt.Fprintf(body, "a kind this build does not read appears in %d of %d, so a bench over them is thinner than what was recorded: %s\n",
			len(thinned), len(read.Sessions), strings.Join(thinned, "; "))
	}
	if len(read.Unreadable) > 0 {
		fmt.Fprintf(body, "%d entries in %s are not readable sessions at all: %s\n",
			len(read.Unreadable), read.Dir, strings.Join(read.Unreadable, "; "))
	}
	return body.String()
}

func kindList(kinds []session.EventKind) string {
	named := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		named = append(named, string(kind))
	}
	slices.Sort(named)
	return strings.Join(named, ", ")
}

func skippedEventGaps(dir, id string) []string {
	reading, err := session.NewStore(dir).Reading(id)
	if err != nil {
		return []string{fmt.Sprintf("skipped events: session %s does not read back, so what the corpus reader skipped is unknown: %v", id, err)}
	}
	if len(reading.Unknown) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("skipped events: session %s carries %s, a kind this build does not read, so this row is measured from a thinner session than the arm recorded", id, kindList(reading.Unknown))}
}
