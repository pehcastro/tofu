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
	"slices"
	"strings"
)

func frameworkOfDependency() map[string]string {
	return map[string]string{
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
	}
}

func knownFrameworks() []string {
	return slices.Compact(slices.Sorted(maps.Values(frameworkOfDependency())))
}

type packageJSON struct {
	Dependencies     map[string]json.RawMessage `json:"dependencies"`
	DevDependencies  map[string]json.RawMessage `json:"devDependencies"`
	PeerDependencies map[string]json.RawMessage `json:"peerDependencies"`
}

func Frameworks(root string, paths []string) ([]string, error) {
	manifests := []string{"package.json"}
	for _, p := range paths {
		literal, _, _ := strings.Cut(filepath.ToSlash(p), "*")
		for dir := path.Dir(literal); dir != "." && dir != "/" && !strings.HasPrefix(dir, ".."); dir = path.Dir(dir) {
			if _, err := os.Stat(filepath.Join(root, dir, "package.json")); err == nil {
				manifests = append(manifests, path.Join(dir, "package.json"))
				break
			}
		}
	}
	listed := map[string]bool{}
	for _, manifest := range manifests {
		at := filepath.Join(root, manifest)
		data, err := os.ReadFile(at)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var declared packageJSON
		if err == nil {
			err = json.Unmarshal(data, &declared)
		}
		if err != nil {
			return nil, fmt.Errorf("%s names the project's frameworks and cannot be read: %w", at, err)
		}
		for dependency, framework := range frameworkOfDependency() {
			listed[framework] = listed[framework] || declared.Dependencies[dependency] != nil || declared.DevDependencies[dependency] != nil || declared.PeerDependencies[dependency] != nil
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
