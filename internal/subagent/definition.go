package subagent

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
)

const (
	AssignmentFile = "agent-models.yaml"
	libraryOrigin  = "library"
	inheritModel   = "inherit"
	disabledModel  = "none"
)

type Runs string

const (
	RunsModel    Runs = "model"
	RunsInherit  Runs = "inherit"
	RunsDisabled Runs = "disabled"
	RunsRefused  Runs = "refused"
)

type Definition struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Origin       string       `json:"origin"`
	Path         string       `json:"path"`
	Runs         Runs         `json:"runs"`
	Model        string       `json:"model,omitempty"`
	Written      string       `json:"written_model,omitempty"`
	AssignedIn   string       `json:"assigned_in,omitempty"`
	Effort       llm.Effort   `json:"effort,omitempty"`
	Tools        []string     `json:"tools,omitempty"`
	Instructions string       `json:"instructions"`
	Refused      []string     `json:"refused,omitempty"`
	Ignored      []string     `json:"ignored,omitempty"`
	IgnoredTools []string     `json:"ignored_tools,omitempty"`
	Shadowed     []Definition `json:"shadowed,omitempty"`
}

type Broken struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type Found struct {
	Definitions []Definition `json:"definitions"`
	Broken      []Broken     `json:"broken,omitempty"`
	Notices     []string     `json:"notices,omitempty"`
}

type Scan struct {
	Project string
	Home    string
	Sources []string
	Library fs.FS
	Tools   []string
	Catalog models.Library
}

type assignment struct {
	model string
	path  string
}

func Definitions(scan Scan) Found {
	layers, notices := scan.layers()
	assigned, broken := scan.assignments()
	found := Found{Broken: broken, Notices: notices}
	var files []file
	for _, layer := range layers {
		glob := "*.md"
		if layer.Name == libraryOrigin {
			glob = "*/agents/*.md"
		}
		matches, _ := fs.Glob(layer.FS, glob)
		for _, match := range matches {
			files = append(files, file{layer: layer, match: match})
		}
	}
	var reading sync.WaitGroup
	for i := range files {
		reading.Go(func() { files[i].definition, files[i].err = read(files[i].layer.FS, files[i].match) })
	}
	reading.Wait()
	index := map[string]int{}
	for _, one := range files {
		path := filepath.Join(one.layer.Origin, filepath.FromSlash(one.match))
		if one.err != nil {
			found.Broken = append(found.Broken, Broken{Path: path, Reason: one.err.Error()})
			continue
		}
		definition := one.definition
		definition.Origin, definition.Path = one.layer.Name, path
		scan.resolve(&definition, assigned[definition.Name])
		if at, seen := index[definition.Name]; seen {
			found.Definitions[at].Shadowed = append(found.Definitions[at].Shadowed, definition)
			continue
		}
		index[definition.Name] = len(found.Definitions)
		found.Definitions = append(found.Definitions, definition)
	}
	return found
}

type file struct {
	layer      sys.Layer
	match      string
	definition Definition
	err        error
}

func (s Scan) layers() ([]sys.Layer, []string) {
	var layers []sys.Layer
	for _, source := range s.Sources {
		layers = append(layers, sys.DirLayer("."+source, filepath.Join(s.Project, "."+source, "agents")))
	}
	var notices []string
	if s.Home == "" {
		notices = append(notices, "no home directory is known, so no personal agent folder was read")
	} else {
		for _, source := range s.Sources {
			layers = append(layers, sys.DirLayer("~/."+source, filepath.Join(s.Home, "."+source, "agents")))
		}
	}
	return append(layers, sys.Layer{Name: libraryOrigin, Origin: libraryOrigin, FS: s.Library}), notices
}

func (s Scan) assignments() (map[string]assignment, []Broken) {
	paths := []string{filepath.Join(s.Project, sys.StateDirName, AssignmentFile)}
	if s.Home != "" {
		paths = append(paths, filepath.Join(s.Home, sys.StateDirName, AssignmentFile))
	}
	assigned := map[string]assignment{}
	var broken []Broken
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var rows map[string][]string
		if err == nil {
			rows, err = fieldsOf(string(data))
		}
		if err != nil {
			broken = append(broken, Broken{Path: path, Reason: err.Error()})
			continue
		}
		for name, model := range rows {
			if _, taken := assigned[name]; !taken {
				assigned[name] = assignment{model: strings.Join(model, " "), path: path}
			}
		}
	}
	return assigned, broken
}

func read(fsys fs.FS, name string) (Definition, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Definition{}, err
	}
	rest, opened := strings.CutPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n")
	if !opened {
		return Definition{}, errors.New("the file opens with no front matter")
	}
	header, body, closed := strings.Cut("\n"+rest, "\n---")
	if !closed {
		return Definition{}, errors.New("the front matter is never closed")
	}
	fields, err := fieldsOf(header)
	if err != nil {
		return Definition{}, err
	}
	effort := fields["effort"]
	if effort == nil {
		effort = fields["thinking"]
	}
	definition := Definition{
		Name:         strings.Join(fields["name"], " "),
		Description:  strings.Join(fields["description"], " "),
		Written:      strings.Join(fields["model"], " "),
		Effort:       llm.Effort(strings.Join(effort, " ")),
		Instructions: strings.TrimSpace(strings.TrimLeft(body, "-")),
	}
	for _, item := range fields["tools"] {
		for tool := range strings.SplitSeq(item, ",") {
			if tool = strings.TrimSpace(tool); tool != "" {
				definition.Tools = append(definition.Tools, tool)
			}
		}
	}
	for key := range fields {
		switch key {
		case "name", "description", "model", "tools", "effort", "thinking":
		default:
			definition.Ignored = append(definition.Ignored, key)
		}
	}
	slices.Sort(definition.Ignored)
	switch {
	case definition.Name == "":
		return Definition{}, errors.New("the front matter has no name")
	case definition.Description == "":
		return Definition{}, errors.New("the front matter has no description")
	}
	return definition, nil
}

func fieldsOf(text string) (map[string][]string, error) {
	fields := map[string][]string{}
	key := ""
	for number, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case line[0] == ' ' || line[0] == '\t':
			if key == "" {
				return nil, fmt.Errorf("line %d is indented under no field", number+1)
			}
			item, _ := strings.CutPrefix(trimmed, "- ")
			fields[key] = append(fields[key], unquote(item))
		default:
			name, value, found := strings.Cut(line, ":")
			if !found {
				return nil, fmt.Errorf("line %d is not a field: %q", number+1, line)
			}
			key = strings.TrimSpace(name)
			fields[key] = valuesOf(strings.TrimSpace(value))
		}
	}
	return fields, nil
}

func valuesOf(value string) []string {
	switch {
	case value == "" || value == "|" || value == ">" || value == "|-" || value == ">-":
		return nil
	case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
		var items []string
		for item := range strings.SplitSeq(value[1:len(value)-1], ",") {
			items = append(items, unquote(strings.TrimSpace(item)))
		}
		return items
	}
	return []string{unquote(value)}
}

func unquote(value string) string {
	if unquoted, err := strconv.Unquote(value); err == nil && strings.HasPrefix(value, `"`) {
		return unquoted
	}
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	return value
}

func (s Scan) resolve(definition *Definition, assigned assignment) {
	tofuWrote := definition.Origin == ".tofu" || definition.Origin == "~/.tofu" || definition.Origin == libraryOrigin
	var tools []string
	for _, written := range definition.Tools {
		tool := written
		if !tofuWrote {
			tool = cmp.Or(claudeTool(written), written)
		}
		switch {
		case slices.Contains(tools, tool):
		case slices.Contains(s.Tools, tool):
			tools = append(tools, tool)
		case tofuWrote:
			definition.Refused = append(definition.Refused, fmt.Sprintf("names the tool %q, which tofu does not have", tool))
		default:
			definition.IgnoredTools = append(definition.IgnoredTools, written)
		}
	}
	definition.Tools = tools
	if definition.Effort != "" {
		if _, err := llm.ParseEffort(string(definition.Effort)); err != nil {
			definition.Refused = append(definition.Refused, err.Error())
		}
	}
	chosen := definition.Written
	if assigned.model != "" {
		chosen, definition.AssignedIn = assigned.model, assigned.path
	}
	var err error
	if definition.Runs, definition.Model, err = s.runsOn(chosen); err != nil {
		definition.Refused = append(definition.Refused, err.Error())
	}
	if len(definition.Refused) > 0 {
		definition.Runs = RunsRefused
	}
}

func claudeTool(name string) string {
	switch name {
	case "Read":
		return "read"
	case "Write":
		return "write"
	case "Edit", "MultiEdit":
		return "edit"
	case "Grep":
		return "search"
	case "Glob":
		return "glob"
	case "Bash":
		return "bash"
	case "WebFetch":
		return "fetch"
	case "WebSearch":
		return "web_search"
	case "TodoWrite":
		return "plan"
	}
	return ""
}

func (s Scan) runsOn(written string) (Runs, string, error) {
	switch written {
	case "", inheritModel:
		return RunsInherit, "", nil
	case disabledModel:
		return RunsDisabled, "", nil
	case "opus", "sonnet", "haiku":
		var matches []string
		for _, model := range s.Catalog.Models {
			if model.Subscription == models.ClaudeSub && model.Use != models.UseExcluded && strings.HasPrefix(model.ID, "claude-"+written+"-") {
				matches = append(matches, model.Slug())
			}
		}
		if len(matches) != 1 {
			return RunsRefused, "", fmt.Errorf("the alias %s names %d allowed claude-sub models, not one: %s", written, len(matches), strings.Join(matches, ", "))
		}
		written = matches[0]
	}
	model, err := s.Catalog.Select(written)
	if err != nil {
		return RunsRefused, "", err
	}
	return RunsModel, model.Slug(), nil
}
