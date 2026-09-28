package skill

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"tofu/internal/konst"
)

const heading = "If a skill matches the task, load it first.\n"

type Skill struct {
	Name        string
	Description string
	File        string
	Dir         string
	Hidden      bool
}

type Found struct {
	Skills   []Skill
	Warnings []string
}

func Discover(project, home string) Found {
	var folders []string
	projectFolders := ProjectFolders(project, home)
	for _, kind := range []string{".tofu", ".agents", ".claude"} {
		for _, dir := range projectFolders {
			folders = append(folders, filepath.Join(dir, kind, "skills"))
		}
	}
	if home != "" {
		folders = append(folders, filepath.Join(home, ".tofu", "skills"))
	}
	var found Found
	seen, fileNamed := map[string]bool{}, map[string]string{}
	for _, folder := range folders {
		entries, _ := os.ReadDir(folder)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || (!entry.IsDir() && entry.Type()&os.ModeSymlink == 0) {
				continue
			}
			file := filepath.Join(folder, entry.Name(), "SKILL.md")
			data, err := os.ReadFile(file)
			if err != nil {
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
			fields, _ := frontMatter(string(data))
			one := Skill{
				Name:        cmp.Or(strings.Join(fields["name"], " "), entry.Name()),
				Description: strings.Join(fields["description"], " "),
				File:        file,
				Dir:         filepath.Dir(file),
				Hidden:      strings.Join(fields["hide"], "") == "true" || strings.Join(fields["disable-model-invocation"], "") == "true",
			}
			if one.Description == "" {
				found.Warnings = append(found.Warnings, "the skill "+one.Name+" in "+file+" has no description and is not offered")
				continue
			}
			if earlier, taken := fileNamed[one.Name]; taken {
				found.Warnings = append(found.Warnings, "the skill "+one.Name+" in "+file+" is not offered: "+earlier+" already has that name")
				continue
			}
			fileNamed[one.Name] = file
			found.Skills = append(found.Skills, one)
		}
	}
	slices.SortFunc(found.Skills, func(a, b Skill) int { return strings.Compare(a.Name, b.Name) })
	return found
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

func frontMatter(text string) (map[string][]string, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	rest, opened := strings.CutPrefix(text, "---\n")
	header, body, closed := strings.Cut("\n"+rest, "\n---")
	if !opened || !closed {
		return map[string][]string{}, text
	}
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
	return fields, strings.TrimLeft(strings.TrimLeft(body, "-"), "\n")
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

func Load(skills []Skill, name, path string) (string, error) {
	at := slices.IndexFunc(skills, func(one Skill) bool { return one.Name == name })
	if at < 0 {
		var names []string
		for _, one := range skills {
			names = append(names, one.Name)
		}
		return "", fmt.Errorf("no skill is named %q; the skills are: %s", name, cmp.Or(strings.Join(names, ", "), "none"))
	}
	one := skills[at]
	if path == "" {
		data, err := os.ReadFile(one.File)
		_, body := frontMatter(string(data))
		return body, err
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

func Wanted(agentFile string) []string {
	data, _ := os.ReadFile(agentFile)
	fields, _ := frontMatter(string(data))
	var names []string
	for _, item := range append(fields["skills"], fields["autoloadSkills"]...) {
		for name := range strings.SplitSeq(item, ",") {
			if name = unquote(strings.TrimSpace(name)); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}
