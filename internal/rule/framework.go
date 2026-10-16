package rule

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type manifest struct {
	file         string
	frameworkOf  map[string]string
	dependencies func([]byte) ([]string, error)
}

func manifests() []manifest {
	return []manifest{
		{"package.json", map[string]string{
			"@angular/core": "angular",
			"@sveltejs/kit": "svelte",
			"astro":         "astro",
			"next":          "react",
			"nuxt":          "vue",
			"preact":        "preact",
			"react":         "react",
			"solid-js":      "solid",
			"svelte":        "svelte",
			"vue":           "vue",
		}, packageJSONDependencies},
		{"Cargo.toml", map[string]string{"gpui-ce": "gpui-ce"}, cargoDependencies},
	}
}

func knownFrameworks() []string {
	var known []string
	for _, m := range manifests() {
		known = slices.AppendSeq(known, maps.Values(m.frameworkOf))
	}
	slices.Sort(known)
	return slices.Compact(known)
}

func manifestListing(frameworks ...string) string {
	var files []string
	for _, m := range manifests() {
		for _, framework := range m.frameworkOf {
			if slices.Contains(frameworks, framework) {
				files = append(files, m.file)
				break
			}
		}
	}
	return strings.Join(files, " or ")
}

func packageJSONDependencies(data []byte) ([]string, error) {
	var declared struct {
		Dependencies     map[string]json.RawMessage `json:"dependencies"`
		DevDependencies  map[string]json.RawMessage `json:"devDependencies"`
		PeerDependencies map[string]json.RawMessage `json:"peerDependencies"`
	}
	if err := json.Unmarshal(data, &declared); err != nil {
		return nil, err
	}
	var names []string
	for _, listed := range []map[string]json.RawMessage{declared.Dependencies, declared.DevDependencies, declared.PeerDependencies} {
		names = slices.AppendSeq(names, maps.Keys(listed))
	}
	return names, nil
}

func cargoDependencies(data []byte) ([]string, error) {
	renamed := regexp.MustCompile(`\bpackage\s*=\s*"([^"]+)"`)
	listsDependencies := func(table string) bool {
		return slices.Contains([]string{"dependencies", "dev-dependencies", "build-dependencies"}, table[strings.LastIndex(table, ".")+1:])
	}
	var names []string
	listing, renamable := false, false
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if table, isHeader := strings.CutPrefix(line, "["); isHeader {
			table = strings.Trim(table, "[] ")
			listing, renamable = listsDependencies(table), false
			if dot := strings.LastIndex(table, "."); !listing && dot >= 0 && listsDependencies(table[:dot]) {
				names, renamable = append(names, strings.Trim(table[dot+1:], `"`)), true
			}
			continue
		}
		key, value, assigns := strings.Cut(line, "=")
		switch {
		case !assigns:
		case renamable && strings.TrimSpace(key) == "package":
			names[len(names)-1] = strings.Trim(strings.TrimSpace(value), `"`)
		case listing:
			name, _, _ := strings.Cut(strings.Trim(strings.TrimSpace(key), `"`), ".")
			if found := renamed.FindStringSubmatch(value); found != nil {
				name = found[1]
			}
			names = append(names, name)
		}
	}
	return names, nil
}

func Frameworks(root string, paths []string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dirs := []string{"."}
	for _, p := range paths {
		if filepath.IsAbs(p) {
			if p, err = filepath.Rel(absRoot, p); err != nil {
				continue
			}
		}
		literal, _, _ := strings.Cut(filepath.ToSlash(p), "*")
		for dir := path.Dir(literal); dir != "." && dir != "/" && !strings.HasPrefix(dir, ".."); dir = path.Dir(dir) {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	listed := map[string]bool{}
	for _, dir := range slices.Compact(dirs) {
		for _, m := range manifests() {
			at := filepath.Join(root, dir, m.file)
			data, err := os.ReadFile(at)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			var names []string
			if err == nil {
				names, err = m.dependencies(data)
			}
			if err != nil {
				return nil, fmt.Errorf("%s names the project's frameworks and cannot be read: %w", at, err)
			}
			for _, name := range names {
				if framework, known := m.frameworkOf[name]; known {
					listed[framework] = true
				}
			}
		}
	}
	var frameworks []string
	for _, framework := range knownFrameworks() {
		if listed[framework] {
			frameworks = append(frameworks, framework)
		}
	}
	return frameworks, nil
}
