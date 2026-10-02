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
	"time"

	"tofu/internal/konst"
	"tofu/internal/llm"
	"tofu/internal/llm/models"
	"tofu/internal/sys"
)

const (
	AssignmentFile = "agent-models.yaml"
	libraryOrigin  = "library"
	inheritModel   = "inherit"
	disabledModel  = "none"
	dateInID       = "20060102"
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
	Language     string       `json:"language,omitempty"`
	Domain       string       `json:"domain,omitempty"`
	Tools        []string     `json:"tools,omitempty"`
	Gate         []string     `json:"gate,omitempty"`
	Instructions string       `json:"instructions"`
	Refused      []string     `json:"refused,omitempty"`
	Ignored      []string     `json:"ignored,omitempty"`
	IgnoredTools []string     `json:"ignored_tools,omitempty"`
	Shadowed     []Definition `json:"shadowed,omitempty"`
	From         string       `json:"from,omitempty"`
	Notices      []string     `json:"notices,omitempty"`
	References   []Reference  `json:"references,omitempty"`
	Cut          []string     `json:"cut_references,omitempty"`
}

type Reference struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Text string `json:"-"`
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
	Project    string
	Home       string
	Sources    []string
	Library    fs.FS
	Tools      []string
	KnownTools []string
	Catalog    models.Library
	Tiers      map[Tier]string
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
	switch {
	case s.Home == "":
		notices = append(notices, "no home directory is known, so ~/.tofu/agents, the only agent folder tofu reads from a home, was not read")
	case slices.Contains(s.Sources, "tofu"):
		layers = append(layers, sys.DirLayer("~/"+sys.StateDirName, filepath.Join(sys.StateDir(s.Home), "agents")))
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

func frontMatter(data []byte) (header, body string, err error) {
	rest, opened := strings.CutPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n")
	if !opened {
		return "", "", errors.New("the file opens with no front matter")
	}
	header, body, closed := strings.Cut("\n"+rest, "\n---")
	if !closed {
		return "", "", errors.New("the front matter is never closed")
	}
	return header, strings.TrimSpace(strings.TrimLeft(body, "-")), nil
}

func read(fsys fs.FS, name string) (Definition, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Definition{}, err
	}
	header, body, err := frontMatter(data)
	if err != nil {
		return Definition{}, err
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
		Language:     strings.Join(fields["language"], " "),
		Domain:       strings.Join(fields["domain"], " "),
		Tools:        commaList(fields["tools"]),
		Gate:         commaList(fields["gate"]),
		Instructions: body,
	}
	for _, named := range fields["references"] {
		definition.References = append(definition.References, Reference{Name: named})
	}
	for key := range fields {
		switch key {
		case "name", "description", "model", "tools", "gate", "effort", "thinking", "references", "language", "domain", "skills", "autoloadSkills":
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

func commaList(items []string) []string {
	var listed []string
	for _, item := range items {
		for one := range strings.SplitSeq(item, ",") {
			if one = strings.TrimSpace(one); one != "" {
				listed = append(listed, one)
			}
		}
	}
	return listed
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
	var tools, unbuilt, missing []string
	for _, written := range definition.Tools {
		tool := written
		if !tofuWrote {
			tool = cmp.Or(claudeTool(written), written)
		}
		switch {
		case slices.Contains(tools, tool):
		case slices.Contains(s.Tools, tool):
			tools = append(tools, tool)
		case tofuWrote && slices.Contains(s.KnownTools, tool):
			unbuilt = append(unbuilt, tool)
		case tofuWrote:
			missing = append(missing, tool)
		default:
			definition.IgnoredTools = append(definition.IgnoredTools, written)
		}
	}
	if len(tools) == 0 {
		missing = append(unbuilt, missing...)
	}
	for _, tool := range missing {
		definition.Refused = append(definition.Refused, fmt.Sprintf("names the tool %q, which tofu does not have", tool))
	}
	definition.Tools = tools
	if definition.Effort != "" {
		if _, err := llm.ParseEffort(string(definition.Effort)); err != nil {
			definition.Refused = append(definition.Refused, err.Error())
		}
	}
	s.place(definition, tofuWrote)
	chosen := definition.Written
	definition.From = "file"
	if assigned.model != "" {
		chosen, definition.From, definition.AssignedIn = assigned.model, AssignmentFile, assigned.path
	}
	if err := s.runsOn(definition, chosen, tofuWrote || assigned.model != ""); err != nil {
		definition.Refused = append(definition.Refused, err.Error())
	}
	if len(definition.Refused) > 0 {
		definition.Runs = RunsRefused
	}
}

func (s Scan) place(definition *Definition, tofuWrote bool) {
	named := definition.References
	definition.References = nil
	if !tofuWrote {
		if len(named) > 0 {
			definition.Ignored = append(definition.Ignored, "references")
		}
		if definition.Language != "" {
			definition.Ignored, definition.Language = append(definition.Ignored, "language"), ""
		}
		slices.Sort(definition.Ignored)
		return
	}
	used := 0
	for _, reference := range named {
		found, _ := fs.Glob(s.Library, "*/references/"+reference.Name+".md")
		deep, _ := fs.Glob(s.Library, "*/*/references/"+reference.Name+".md")
		found = append(found, deep...)
		if len(found) == 0 {
			definition.Refused = append(definition.Refused, fmt.Sprintf("names the reference %q, which the library does not ship", reference.Name))
			continue
		}
		data, err := fs.ReadFile(s.Library, found[0])
		var text string
		if err == nil {
			_, text, err = frontMatter(data)
		}
		switch {
		case err != nil:
			definition.Refused = append(definition.Refused, fmt.Sprintf("the reference %q does not read: %v", reference.Name, err))
		case len(definition.Cut) > 0 || used+len(text) > konst.SubAgentReferenceBytes:
			definition.Cut = append(definition.Cut, reference.Name)
		default:
			used += len(text)
			definition.References = append(definition.References, Reference{Name: reference.Name, Path: libraryOrigin + "/" + found[0], Text: text})
		}
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

func (s Scan) runsOn(definition *Definition, written string, tofuFile bool) error {
	named, isTier := strings.CutPrefix(written, "@")
	tier := Tier(named)
	switch {
	case written == "" || written == inheritModel:
		definition.Runs = RunsInherit
		return nil
	case written == disabledModel:
		definition.Runs = RunsDisabled
		return nil
	case isTier && !slices.Contains(Tiers(), tier):
		return fmt.Errorf("names the tier %s, and a tier is one of @%s", written, strings.Join(tierNames(), ", @"))
	case isTier && !tofuFile:
		return fmt.Errorf("names the tier %s in a shared file, which Claude Code cannot open with it: assign the tier in %s instead", written, AssignmentFile)
	case !isTier && !tofuFile && s.Tiers[aliasTier(written)] != "":
		tier, isTier = aliasTier(written), true
	}
	slug, unresolved := written, ""
	switch {
	case isTier:
		definition.From, slug = "tier "+string(tier), s.Tiers[tier]
		if slug == "" {
			unresolved = fmt.Sprintf("the tier @%s is not set, so %s runs on the orchestrator's model: tofu settings set %s <slug>", tier, definition.Name, tier.Setting())
		}
	case aliasTier(written) != "":
		var family []models.Model
		for _, model := range s.Catalog.Models {
			if model.Subscription == models.ClaudeSub && model.Use != models.UseExcluded && strings.HasPrefix(model.ID, "claude-"+written+"-") {
				family = append(family, model)
			}
		}
		if len(family) == 0 {
			unresolved = fmt.Sprintf("the alias %s names no allowed claude-sub model, so %s runs on the orchestrator's model", written, definition.Name)
			break
		}
		slug = slices.MaxFunc(family, func(a, b models.Model) int { return slices.Compare(versionOf(a.ID), versionOf(b.ID)) }).Slug()
	}
	if unresolved != "" {
		definition.Runs, definition.Notices = RunsInherit, append(definition.Notices, unresolved)
		return nil
	}
	model, err := s.Catalog.Select(slug)
	switch {
	case err != nil && isTier:
		return fmt.Errorf("the tier @%s is %s by the setting %s: %w", tier, slug, tier.Setting(), err)
	case err != nil:
		return err
	}
	definition.Runs, definition.Model = RunsModel, model.Slug()
	return nil
}

func versionOf(id string) []int {
	var version []int
	for _, part := range strings.Split(id, "-") {
		if _, err := time.Parse(dateInID, part); err == nil {
			continue
		}
		if number, err := strconv.Atoi(part); err == nil {
			version = append(version, number)
		}
	}
	return version
}
