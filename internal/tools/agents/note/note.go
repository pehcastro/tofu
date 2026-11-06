package note

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const (
	Name     = "note"
	listLead = "the notes you keep, carried word for word through every fork and compaction; this list replaces any earlier one:\n"
	noneLead = "you keep no notes now; this replaces any earlier list\n"
)

type kept struct {
	id   int
	text string
}

type Notes struct {
	held    []kept
	next    int
	changed bool
	mutex   sync.Mutex
}

type notesKey struct{}

func Origin() core.Origin {
	return core.Origin{Source: core.SourceNotice, Name: Name}
}

func Kept(message core.Message) bool {
	return message.Role == core.RoleUser && message.Origin.Source == core.SourceNotice && message.Origin.Name == Name
}

func Latest(messages []core.Message) (core.Message, bool) {
	for _, message := range slices.Backward(messages) {
		if Kept(message) {
			return message, true
		}
	}
	return core.Message{}, false
}

func Hold(messages []core.Message) *Notes {
	notes := &Notes{next: 1}
	latest, _ := Latest(messages)
	listed, _ := strings.CutPrefix(latest.Content, listLead)
	for line := range strings.Lines(listed) {
		number, text, cut := strings.Cut(strings.TrimSuffix(line, "\n"), ": ")
		id, err := strconv.Atoi(number)
		if !cut || err != nil {
			continue
		}
		notes.held = append(notes.held, kept{id: id, text: text})
		notes.next = max(notes.next, id+1)
	}
	return notes
}

func With(ctx context.Context, notes *Notes) context.Context {
	return context.WithValue(ctx, notesKey{}, notes)
}

func (n *Notes) listed() string {
	if len(n.held) == 0 {
		return noneLead
	}
	var said strings.Builder
	said.WriteString(listLead)
	for _, note := range n.held {
		fmt.Fprintf(&said, "%d: %s\n", note.id, note.text)
	}
	return said.String()
}

func (n *Notes) Changed() (string, bool) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return n.listed(), n.changed
}

type Note struct{}

func (Note) Name() string { return Name }

func (Note) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "keeps a short fact in your own context, word for word, through every fork and compaction of this session, from your next request on: a decision, a constraint, a path, a number you must not lose. " +
			"text adds one note of one line, at most " + strconv.Itoa(konst.NoteBytes) + " bytes; done drops the note with that id once it no longer holds; with neither it lists your notes. " +
			"at most " + strconv.Itoa(konst.NotesMost) + " notes are kept, and past that a new one is refused until you mark one done. " +
			"a note lasts for this session and its forks; a rule the person wants kept for later sessions is remember's, not this.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
				"done": map[string]any{"type": "integer"},
			},
		},
	}
}

func (Note) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Text *string `json:"text"`
		Done *int    `json:"done"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("note: arguments are not the expected shape: %w", err)
	}
	n, inTurn := ctx.Value(notesKey{}).(*Notes)
	if !inTurn {
		return contract.Result{}, errors.New("note: no turn is running, so there is no context to keep a note in; the model calls note inside a turn")
	}
	n.mutex.Lock()
	defer n.mutex.Unlock()
	switch {
	case args.Text != nil && args.Done != nil:
		return contract.Result{}, errors.New("note: give text to add a note or done to drop one, not both")
	case args.Text != nil:
		return n.add(strings.TrimSpace(*args.Text))
	case args.Done != nil:
		return n.done(*args.Done)
	}
	return contract.Result{Content: n.listed(), Command: Name + " list"}, nil
}

func (n *Notes) add(text string) (contract.Result, error) {
	switch {
	case text == "":
		return contract.Result{}, errors.New("note: text is empty; nothing was kept")
	case strings.ContainsAny(text, "\r\n"):
		return contract.Result{}, errors.New("note: a note is one line; write each fact as its own note, so nothing was kept")
	case len(text) > konst.NoteBytes:
		return contract.Result{}, fmt.Errorf("note: the text is %d bytes, over the %d a note holds; shorten it, so nothing was kept", len(text), konst.NoteBytes)
	}
	command := Name + " " + text
	if at := slices.IndexFunc(n.held, func(note kept) bool { return note.text == text }); at >= 0 {
		return contract.Result{Content: fmt.Sprintf("already kept as note %d", n.held[at].id), Command: command}, nil
	}
	if len(n.held) >= konst.NotesMost {
		return contract.Result{}, fmt.Errorf("note: %d notes are kept, the most there can be, so this one was refused and nothing was dropped; the oldest is %d: %s. mark one done first",
			len(n.held), n.held[0].id, n.held[0].text)
	}
	n.held = append(n.held, kept{id: n.next, text: text})
	n.next++
	n.changed = true
	return contract.Result{Content: fmt.Sprintf("kept as note %d; your next request carries it", n.next-1), Command: command}, nil
}

func (n *Notes) done(id int) (contract.Result, error) {
	at := slices.IndexFunc(n.held, func(note kept) bool { return note.id == id })
	if at < 0 {
		return contract.Result{}, fmt.Errorf("note: no note %d is kept; with neither text nor done, note lists them", id)
	}
	said := fmt.Sprintf("note %d done; your next request no longer carries it: %s", id, n.held[at].text)
	n.held = slices.Delete(n.held, at, at+1)
	n.changed = true
	return contract.Result{Content: said, Command: Name + " done " + strconv.Itoa(id)}, nil
}
