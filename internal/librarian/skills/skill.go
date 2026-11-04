package skills

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
)

const heading = "If a skill matches the task, load it first.\n"

type Origin string

const (
	OriginProject Origin = "project"
	OriginHome    Origin = "home"
	OriginLibrary Origin = "library"
)

type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Domain      string   `json:"domain,omitempty"`
	Origin      Origin   `json:"origin"`
	File        string   `json:"path"`
	Dir         string   `json:"dir"`
	Hidden      bool     `json:"hidden"`
	References  []string `json:"references,omitempty"`
}

type Found struct {
	Skills    []Skill
	Unoffered []Unoffered
	Warnings  []string
}

type Unoffered struct {
	Name, File, Why string
}

func (f *Found) unoffer(name, file, why string) {
	f.Unoffered = append(f.Unoffered, Unoffered{Name: name, File: file, Why: why})
	f.Warnings = append(f.Warnings, "the skill "+name+" in "+file+" is not offered: "+why)
}

type folder struct {
	dir    string
	origin Origin
}

func Discover(project, home string) Found {
	var folders []folder
	projectFolders := ProjectFolders(project, home)
	for _, kind := range []string{".tofu", ".agents", ".claude"} {
		for _, dir := range projectFolders {
			folders = append(folders, folder{filepath.Join(dir, kind, "skills"), OriginProject})
		}
	}
	if home != "" {
		folders = append(folders, folder{filepath.Join(home, ".tofu", "skills"), OriginHome})
	}
	var found Found
	seen, fileNamed := map[string]bool{}, map[string]string{}
	for _, folder := range folders {
		entries, _ := os.ReadDir(folder.dir)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || (!entry.IsDir() && entry.Type()&os.ModeSymlink == 0) {
				continue
			}
			file := filepath.Join(folder.dir, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(file)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if failed := (*fs.PathError)(nil); errors.As(err, &failed) {
				found.unoffer(entry.Name(), file, "it did not read: "+failed.Err.Error())
				continue
			}
			real, err := filepath.EvalSymlinks(file)
			if err != nil {
				real = file
			}
			if seen[real] {
				continue
			}
			seen[real] = true
			fields, _ := FrontMatter(string(data))
			one := Skill{
				Name:        cmp.Or(strings.Join(fields["name"], " "), entry.Name()),
				Description: strings.Join(fields["description"], " "),
				Origin:      folder.origin,
				File:        file,
				Dir:         filepath.Dir(file),
				Hidden:      strings.Join(fields["hide"], "") == "true" || strings.Join(fields["disable-model-invocation"], "") == "true",
			}
			if one.Description == "" {
				found.unoffer(one.Name, file, "it has no description")
				continue
			}
			if earlier, taken := fileNamed[one.Name]; taken {
				found.unoffer(one.Name, file, earlier+" already has that name")
				continue
			}
			fileNamed[one.Name] = file
			found.Skills = append(found.Skills, one)
		}
	}
	slices.SortFunc(found.Skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return found
}

func Shipped(library fs.FS, root string) ([]Skill, error) {
	var shipped []Skill
	err := fs.WalkDir(library, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || path.Base(path.Dir(name)) != "skills" || path.Ext(name) != ".md" {
			return err
		}
		data, err := fs.ReadFile(library, name)
		if err != nil {
			return err
		}
		fields, _ := FrontMatter(string(data))
		file := path.Join(root, name)
		shipped = append(shipped, Skill{Name: cmp.Or(strings.Join(fields["name"], " "), strings.TrimSuffix(path.Base(name), ".md")),
			Description: strings.Join(fields["description"], " "), Domain: strings.Join(fields["domain"], " "), Origin: OriginLibrary, File: file, Dir: path.Dir(file), References: fields["references"]})
		return nil
	})
	slices.SortFunc(shipped, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return shipped, err
}

func ReferenceFiles(library fs.FS, owner string, names []string) ([]string, error) {
	var files []string
	for _, name := range names {
		found := LibraryFiles(library, "references", name)
		if len(found) != 1 {
			return nil, fmt.Errorf("the skill %s names the reference %s, which the library ships %d times", owner, name, len(found))
		}
		files = append(files, found[0])
	}
	return files, nil
}

func LibraryFiles(library fs.FS, folder, name string) []string {
	found, _ := fs.Glob(library, "*/"+folder+"/"+name+".md")
	deeper, _ := fs.Glob(library, "*/*/"+folder+"/"+name+".md")
	return append(found, deeper...)
}

func ProjectFolders(project, home string) []string {
	project, _ = filepath.Abs(project)
	var dirs []string
	for dir := project; ; {
		if dir != home {
			dirs = append(dirs, dir)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dirs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dirs[:min(1, len(dirs))]
		}
		dir = parent
	}
}

func SplitFrontMatter(text string) (header, body string, found bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, opened := strings.CutPrefix(text, "---\n")
	header, body, closed := strings.Cut("\n"+rest, "\n---")
	if !opened || !closed {
		return "", text, false
	}
	return strings.TrimPrefix(header, "\n"), strings.TrimLeft(strings.TrimLeft(body, "-"), "\n"), true
}

func FrontMatter(text string) (map[string][]string, string) {
	header, body, _ := SplitFrontMatter(text)
	fields, key, folding := map[string][]string{}, "", false
	for _, line := range strings.Split(header, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		case line[0] == ' ' || line[0] == '\t':
			if !folding {
				trimmed = strings.TrimPrefix(trimmed, "- ")
			}
			fields[key] = append(fields[key], unquote(trimmed))
		default:
			name, value, _ := strings.Cut(trimmed, ":")
			key, value = strings.TrimSpace(name), strings.TrimSpace(value)
			folding = strings.HasPrefix(value, ">") || strings.HasPrefix(value, "|")
			if !folding && value != "" {
				fields[key] = []string{unquote(strings.Trim(value, "[]"))}
			}
		}
	}
	return fields, body
}

func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

func Listing(skills []Skill, windowTokens int) string {
	budget := konst.SkillListingUnknownCharacters
	if windowTokens > 0 {
		budget = windowTokens * konst.SkillListingWindowPercent / 100 * konst.SkillListingCharactersPerToken
	}
	var full strings.Builder
	var names []string
	for _, one := range skills {
		if one.Hidden {
			continue
		}
		description := one.Description
		if utf8.RuneCountInString(description) > konst.SkillDescriptionCharacters {
			description = string([]rune(description)[:konst.SkillDescriptionCharacters])
		}
		full.WriteString("- " + one.Name + ": " + description + "\n")
		names = append(names, one.Name)
	}
	switch {
	case len(names) == 0:
		return ""
	case utf8.RuneCountInString(full.String()) <= budget:
		return heading + strings.TrimSuffix(full.String(), "\n")
	}
	var kept strings.Builder
	used, omitted := 0, 0
	for _, name := range names {
		line := "- " + name + "\n"
		if used += utf8.RuneCountInString(line); used > budget {
			omitted++
			continue
		}
		kept.WriteString(line)
	}
	note := fmt.Sprintf("%d descriptions were cut to fit %d characters; load a skill to read what it does.", len(names), budget)
	if omitted > 0 {
		note += fmt.Sprintf(" %d skills did not fit even by name and are not listed.", omitted)
	}
	return heading + kept.String() + note
}

func Find(skills []Skill, name string) (Skill, error) {
	at := slices.IndexFunc(skills, func(one Skill) bool { return one.Name == name })
	if at < 0 {
		var names []string
		for _, one := range skills {
			names = append(names, one.Name)
		}
		return Skill{}, fmt.Errorf("no skill is named %q; the skills are: %s", name, cmp.Or(strings.Join(names, ", "), "none"))
	}
	return skills[at], nil
}

func Body(library fs.FS, one Skill) (string, error) {
	var data []byte
	var err error
	if one.Origin == OriginLibrary {
		_, inLibrary, _ := strings.Cut(one.File, "/")
		data, err = fs.ReadFile(library, inLibrary)
	} else {
		data, err = os.ReadFile(one.File)
	}
	_, body := FrontMatter(string(data))
	return body, err
}

func Load(skills []Skill, library fs.FS, name, path string) (string, error) {
	one, err := Find(skills, name)
	if err != nil {
		return "", err
	}
	if one.Origin == OriginLibrary {
		if path != "" {
			return "", fmt.Errorf("the library skill %s is one file, so it has no %s to read", name, path)
		}
		if _, err := ReferenceFiles(library, name, one.References); err != nil {
			return "", err
		}
		body, err := Body(library, one)
		if len(one.References) > 0 {
			body += "\n\nthis skill names the references " + strings.Join(one.References, ", ") + "; read one whole with the reference tool."
		}
		return body, err
	}
	if path == "" {
		return Body(library, one)
	}
	climbs := slices.Contains(strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }), "..")
	if climbs || filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) || filepath.VolumeName(path) != "" {
		return "", fmt.Errorf("%s is not inside the skill %s: a path is relative to its folder and never climbs out", path, name)
	}
	dir, dirErr := filepath.EvalSymlinks(one.Dir)
	target, targetErr := filepath.EvalSymlinks(filepath.Join(one.Dir, path))
	if err := cmp.Or(dirErr, targetErr); err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s leads out of the skill %s through a link", path, name)
	}
	data, err := os.ReadFile(target)
	return string(data), err
}
