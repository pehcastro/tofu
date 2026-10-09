package boardy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	ViewsFileName = "views.toml"
	CurrentSprint = "active"
	Me            = "me"
)

type GroupBy string

const (
	Ungrouped     GroupBy = ""
	ByEpic        GroupBy = "epic"
	ByStatus      GroupBy = "status"
	ByAssignee    GroupBy = "assignee"
	defaultViewID         = "default"
)

type Filter struct {
	Statuses []Status `json:"statuses,omitempty"`
	Live     bool     `json:"live,omitempty"`
	Sprint   string   `json:"sprint,omitempty"`
	Epic     string   `json:"epic,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Label    string   `json:"label,omitempty"`
	Text     string   `json:"text,omitempty"`
}

type View struct {
	Name    string  `json:"name"`
	Filter  Filter  `json:"filter"`
	GroupBy GroupBy `json:"group_by,omitempty"`
}

func builtinViews() []View {
	return []View{
		{Name: defaultViewID, Filter: Filter{Live: true, Sprint: CurrentSprint}, GroupBy: ByStatus},
		{Name: "review", Filter: Filter{Statuses: []Status{Review}}},
		{Name: "epic", Filter: Filter{Live: true}, GroupBy: ByEpic},
		{Name: "blocked", Filter: Filter{Statuses: []Status{Blocked}}},
		{Name: "mine", Filter: Filter{Live: true, Assignee: Me}, GroupBy: ByStatus},
		{Name: "all", GroupBy: ByStatus},
	}
}

func (s Store) Views(key string) ([]View, error) {
	views := builtinViews()
	raw, err := os.ReadFile(filepath.Join(s.BoardDir(key), ViewsFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return views, nil
	}
	if err != nil {
		return views, err
	}
	for line := range strings.Lines(strings.ReplaceAll(string(raw), "\r\n", "\n")) {
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if name = strings.TrimSpace(name); !found || !validPlanID(name) {
			return views, fmt.Errorf("%s line %q is not name = {view}", ViewsFileName, line)
		}
		var view View
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&view); err != nil {
			return views, fmt.Errorf("%s %s: %w", ViewsFileName, name, err)
		}
		if !slices.Contains([]GroupBy{Ungrouped, ByEpic, ByStatus, ByAssignee}, view.GroupBy) {
			return views, fmt.Errorf("%s %s: group_by %q is not epic, status or assignee", ViewsFileName, name, view.GroupBy)
		}
		view.Name = name
		views = append(slices.DeleteFunc(views, func(known View) bool { return known.Name == name }), view)
	}
	return views, nil
}

func (s Store) View(key, name string) (View, error) {
	if name == "" {
		name = defaultViewID
	}
	views, err := s.Views(key)
	if index := slices.IndexFunc(views, func(view View) bool { return view.Name == name }); index >= 0 {
		return views[index], err
	}
	names := make([]string, len(views))
	for i, view := range views {
		names[i] = view.Name
	}
	return View{}, errors.Join(err, fmt.Errorf("the board %s has no view %q: %s", key, name, strings.Join(names, ", ")))
}

type Resolved struct {
	Sprint string
	Me     string
}

func (f Filter) Match(ticket Ticket, resolved Resolved) bool {
	sprint := f.Sprint
	if sprint == CurrentSprint {
		sprint = resolved.Sprint
	}
	assignee := f.Assignee
	if assignee == Me {
		assignee = resolved.Me
	}
	text := strings.ToLower(f.Text)
	return (len(f.Statuses) == 0 || slices.Contains(f.Statuses, ticket.Status)) &&
		(!f.Live || ticket.Status.Live()) &&
		(sprint == "" || ticket.Sprint == sprint) &&
		(f.Epic == "" || ticket.Epic == f.Epic) &&
		(f.Assignee == "" || ticket.Assignee == assignee) &&
		(f.Label == "" || slices.Contains(ticket.Labels, f.Label)) &&
		(text == "" || strings.Contains(strings.ToLower(ticket.ID+" "+ticket.Title), text))
}

type Group struct {
	Name    string   `json:"name"`
	Tickets []Ticket `json:"tickets"`
}

func Apply(view View, extra Filter, tickets []Ticket, resolved Resolved) []Group {
	var kept []Ticket
	for _, ticket := range tickets {
		if view.Filter.Match(ticket, resolved) && extra.Match(ticket, resolved) {
			kept = append(kept, ticket)
		}
	}
	slices.SortStableFunc(kept, func(a, b Ticket) int { return strings.Compare(string(a.Priority), string(b.Priority)) })
	groups := []Group{}
	for _, ticket := range kept {
		name := groupName(view.GroupBy, ticket)
		index := slices.IndexFunc(groups, func(group Group) bool { return group.Name == name })
		if index < 0 {
			groups, index = append(groups, Group{Name: name}), len(groups)
		}
		groups[index].Tickets = append(groups[index].Tickets, ticket)
	}
	slices.SortStableFunc(groups, func(a, b Group) int {
		if view.GroupBy == ByStatus {
			return slices.Index(Statuses(), Status(a.Name)) - slices.Index(Statuses(), Status(b.Name))
		}
		return strings.Compare(a.Name, b.Name)
	})
	return groups
}

func groupName(by GroupBy, ticket Ticket) string {
	switch by {
	case Ungrouped:
		return ""
	case ByEpic:
		return ticket.Epic
	case ByStatus:
		return string(ticket.Status)
	case ByAssignee:
		return ticket.Assignee
	}
	panic("boardy: unknown group " + string(by))
}
