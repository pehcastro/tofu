package learn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"tofu/internal/sys"
)

type DraftKind string

const (
	DraftFailure DraftKind = "failure"
	DraftBug     DraftKind = "bug"
	DraftLibrary DraftKind = "library"
)

const (
	draftFormat  = "tofu-learn-upstream"
	draftVersion = 1
	markField    = "mark"
	frontFence   = "---"
	markScheme   = "hmac-sha256 over every other line of this file, keyed by a secret only tofu learn on this install holds"
)

type Build struct {
	Version  string
	Commit   string
	Platform string
}

type Draft struct {
	Kind        DraftKind `json:"kind"`
	Key         string    `json:"key"`
	Mechanism   string    `json:"mechanism"`
	Build       Build     `json:"-"`
	Written     time.Time `json:"written"`
	Sessions    int       `json:"sessions"`
	Recurrences int       `json:"recurrences"`
	Present     int       `json:"in_request_when_recurred"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Mark        string    `json:"mark"`
}

func DraftOf(p Proposal, build Build, at time.Time) Draft {
	draft := Draft{Kind: DraftFailure, Key: p.Key, Mechanism: p.Mechanism, Build: build, Written: at, Sessions: p.Places}
	var examples []string
	for _, theme := range p.Themes {
		draft.Recurrences += len(theme.Checks)
		draft.Present += theme.Present()
		examples = append(examples, fmt.Sprintf("- the person corrected how the lead behaved (%s) %d times in %d sessions; the earlier correction was in the request the model answered %d of %d times it came back.",
			theme.Label(), len(theme.Quotes), theme.Places, theme.Present(), len(theme.Checks)))
	}
	happened, did, expected := "", "", ""
	switch p.Mechanism {
	case forkCarry:
		draft.Title = "A fork drops what the person said, and the lead repeats a mistake the person had already corrected"
		happened = "across " + strconv.Itoa(p.Places) + " sessions of one chain, the person corrected the lead, the chain forked, and the lead made the corrected mistake again. In " +
			strconv.Itoa(draft.Recurrences-draft.Present) + " of " + strconv.Itoa(draft.Recurrences) + " recurrences the earlier correction was not in the request the model answered."
		did, expected = "the fork carry that starts the next session did not hold the person's earlier words.", "what the person asked for still holds after every fork."
	case leadTurnEnd:
		draft.Title = "The lead ends a turn handing the person a choice it could have made, and the person corrects it"
		happened = "in " + strconv.Itoa(p.Places) + " sessions the lead ended its turn asking the person to decide or whether to go on, and the person's next message corrected it."
		did, expected = "only a sub-agent's turn end is judged; nothing judges whether the lead should end its turn.", "the lead keeps working on what it can do and asks only what only the person can decide."
	default:
		panic("learn: no draft for the mechanism " + p.Mechanism)
	}
	draft.Body = strings.Join([]string{"What happened: " + happened, "", "Examples, in general terms:", strings.Join(examples, "\n"), "",
		"What tofu did: " + did, "What was expected: " + expected, "",
		"Not included: quotes, session names, transcripts, file contents, paths, project names. Not sent."}, "\n")
	return draft
}

func (d Draft) Encode(key []byte) string {
	front := []string{frontFence, "format: " + draftFormat, "format_version: " + strconv.Itoa(draftVersion), "kind: " + string(d.Kind), "key: " + d.Key,
		"mechanism: " + d.Mechanism, "tofu: " + d.Build.Version, "commit: " + d.Build.Commit, "platform: " + d.Build.Platform,
		"written: " + d.Written.Format(time.RFC3339), "sessions: " + strconv.Itoa(d.Sessions), "recurrences: " + strconv.Itoa(d.Recurrences),
		"in_request_when_recurred: " + strconv.Itoa(d.Present), "mark_scheme: " + markScheme}
	rest := []string{frontFence, "# " + d.Title, "", d.Body, ""}
	mark := markOf(key, slices.Concat(front, rest))
	return strings.Join(slices.Concat(front, []string{markField + ": " + mark}, rest), "\n")
}

func markOf(key []byte, lines []string) string {
	sum := hmac.New(sha256.New, key)
	sum.Write([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum.Sum(nil))
}

func ReadDraft(data []byte, key []byte) (Draft, bool) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var draft Draft
	var kept []string
	for i, line := range lines {
		field, value, _ := strings.Cut(line, ": ")
		switch {
		case field == markField && draft.Mark == "":
			draft.Mark = value
			continue
		case field == "kind":
			draft.Kind = DraftKind(value)
		case field == "key":
			draft.Key = value
		case field == "mechanism":
			draft.Mechanism = value
		case field == "sessions":
			draft.Sessions, _ = strconv.Atoi(value)
		case field == "written":
			draft.Written, _ = time.Parse(time.RFC3339, value)
		case strings.HasPrefix(line, "# ") && draft.Title == "":
			draft.Title = strings.TrimPrefix(line, "# ")
			draft.Body = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
		kept = append(kept, line)
	}
	return draft, draft.Mark != "" && hmac.Equal([]byte(draft.Mark), []byte(markOf(key, kept)))
}

func (h Home) markKey() ([]byte, error) {
	file := filepath.Join(h.Dir, keyFile)
	data, err := os.ReadFile(file)
	if err == nil {
		return hex.DecodeString(strings.TrimSpace(string(data)))
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, markKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, sys.WriteFile(file, []byte(hex.EncodeToString(key)+"\n"), 0o600)
}

func (h Home) WriteDraft(d Draft) (string, string, error) {
	key, err := h.markKey()
	if err != nil {
		return "", "", err
	}
	file, text := filepath.Join(h.UpstreamDir(), d.Key+".md"), d.Encode(key)
	return file, text, sys.WriteFile(file, []byte(text), 0o600)
}

type Listed struct {
	File   string `json:"file"`
	Draft  Draft  `json:"draft"`
	Marked bool   `json:"marked"`
}

func (h Home) Drafts() ([]Listed, error) {
	names, err := filepath.Glob(filepath.Join(h.UpstreamDir(), "*.md"))
	if err != nil || len(names) == 0 {
		return nil, err
	}
	key, err := h.markKey()
	if err != nil {
		return nil, err
	}
	var listed []Listed
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		draft, marked := ReadDraft(data, key)
		listed = append(listed, Listed{File: name, Draft: draft, Marked: marked})
	}
	return listed, nil
}
