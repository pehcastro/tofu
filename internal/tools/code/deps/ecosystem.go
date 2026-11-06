package deps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"tofu/internal/tools/contract"
)

type ecosystem int

const (
	goModules ecosystem = iota
	npm
	cargo
	python
)

type dependency struct {
	name, version string
	indirect      bool
}

func ecosystemNamed(name string) (ecosystem, error) {
	for _, e := range []ecosystem{goModules, npm, cargo, python} {
		if e.String() == name {
			return e, nil
		}
	}
	return 0, fmt.Errorf("ecosystem %q is not one of go, npm, cargo, python", name)
}

func (e ecosystem) String() string {
	switch e {
	case goModules:
		return "go"
	case npm:
		return "npm"
	case cargo:
		return "cargo"
	case python:
		return "python"
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) osv() string {
	switch e {
	case goModules:
		return "Go"
	case npm:
		return "npm"
	case cargo:
		return "crates.io"
	case python:
		return "PyPI"
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) depsDev() string {
	switch e {
	case goModules:
		return "go"
	case npm:
		return "npm"
	case cargo:
		return "cargo"
	case python:
		return "pypi"
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) lockfiles() []string {
	switch e {
	case goModules:
		return []string{"go.mod", "go.sum"}
	case npm:
		return []string{"package.json", "package-lock.json"}
	case cargo:
		return []string{"Cargo.toml", "Cargo.lock"}
	case python:
		return []string{"pyproject.toml", "uv.lock"}
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) present(dir string) bool {
	if contract.IsFile(filepath.Join(dir, e.lockfiles()[0])) {
		return true
	}
	requirements, _ := filepath.Glob(filepath.Join(dir, "requirements*.txt"))
	return e == python && len(requirements) > 0
}

func detect(dir, asked string) (ecosystem, error) {
	if asked != "" {
		return ecosystemNamed(asked)
	}
	var found []ecosystem
	for _, e := range []ecosystem{goModules, npm, cargo, python} {
		if e.present(dir) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 0:
		return 0, errors.New("no go.mod, package.json, Cargo.toml, pyproject.toml or requirements file in " + dir)
	case 1:
		return found[0], nil
	}
	names := make([]string, len(found))
	for i, e := range found {
		names[i] = e.String()
	}
	return 0, errors.New("this directory holds " + strings.Join(names, " and ") + " projects. name one in ecosystem")
}

func (e ecosystem) install(pkg, version string) []string {
	switch e {
	case goModules:
		return []string{"go", "get", pkg + "@" + version}
	case npm:
		return []string{"npm", "install", pkg + "@" + version}
	case cargo:
		return []string{"cargo", "add", pkg + "@" + version}
	case python:
		return []string{"uv", "add", pkg + "==" + version}
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) why(pkg string) []string {
	switch e {
	case goModules:
		return []string{"go", "mod", "why", "-m", pkg}
	case npm:
		return []string{"npm", "explain", pkg}
	case cargo:
		return []string{"cargo", "tree", "--invert", pkg}
	case python:
		return []string{"uv", "tree", "--invert", "--package", pkg}
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) registry() string {
	switch e {
	case goModules:
		return "proxy.golang.org"
	case npm:
		return "registry.npmjs.org"
	case cargo:
		return "crates.io"
	case python:
		return "pypi.org"
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) program() (string, error) {
	found, err := exec.LookPath(e.install("", "")[0])
	if err == nil {
		return found, nil
	}
	switch e {
	case goModules:
		return "", errors.New("go is not on PATH. install it from https://go.dev/dl")
	case npm:
		return "", errors.New("npm is not on PATH. install Node.js from https://nodejs.org")
	case cargo:
		return "", errors.New("cargo is not on PATH. install it with rustup from https://rustup.rs")
	case python:
		return "", errors.New("uv is not on PATH, and deps writes python dependencies with uv add. install it with pip install uv or from https://docs.astral.sh/uv")
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (e ecosystem) direct(ctx context.Context, dir string) ([]dependency, error) {
	switch e {
	case goModules:
		command := exec.CommandContext(ctx, "go", "mod", "edit", "-json")
		command.Dir = dir
		out, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("go mod edit -json: %w", err)
		}
		var mod struct {
			Require []struct {
				Path, Version string
				Indirect      bool
			}
		}
		if err := json.Unmarshal(out, &mod); err != nil {
			return nil, fmt.Errorf("go mod edit -json: %w", err)
		}
		found := make([]dependency, len(mod.Require))
		for i, r := range mod.Require {
			found[i] = dependency{name: r.Path, version: r.Version, indirect: r.Indirect}
		}
		return found, nil
	case npm:
		var manifest struct{ Dependencies, DevDependencies map[string]string }
		if err := readInto(filepath.Join(dir, "package.json"), json.Unmarshal, &manifest); err != nil {
			return nil, err
		}
		var found []dependency
		for _, section := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
			for name, spec := range section {
				found = append(found, dependency{name: name, version: concrete(spec)})
			}
		}
		return sorted(found), nil
	case cargo:
		var manifest map[string]map[string]any
		if err := readInto(filepath.Join(dir, "Cargo.toml"), toml.Unmarshal, &manifest); err != nil {
			return nil, err
		}
		var found []dependency
		for _, section := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
			for name, spec := range manifest[section] {
				version, _ := spec.(string)
				if table, ok := spec.(map[string]any); ok {
					version, _ = table["version"].(string)
				}
				found = append(found, dependency{name: name, version: concrete(version)})
			}
		}
		return sorted(found), nil
	case python:
		var lines []string
		var project struct {
			Project struct{ Dependencies []string }
		}
		if err := readInto(filepath.Join(dir, "pyproject.toml"), toml.Unmarshal, &project); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		lines = append(lines, project.Project.Dependencies...)
		requirements, _ := filepath.Glob(filepath.Join(dir, "requirements*.txt"))
		for _, path := range requirements {
			text, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			lines = append(lines, strings.Split(string(text), "\n")...)
		}
		var found []dependency
		for _, line := range lines {
			line, _, _ = strings.Cut(line, "#")
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "-") {
				continue
			}
			name, rest := line, ""
			if cut := strings.IndexAny(line, "<>=!~;[( "); cut >= 0 {
				name, rest = line[:cut], line[cut:]
			}
			if _, afterExtras, extras := strings.Cut(rest, "]"); extras && strings.HasPrefix(rest, "[") {
				rest = afterExtras
			}
			found = append(found, dependency{name: name, version: concrete(rest)})
		}
		return found, nil
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func readInto(path string, decode func([]byte, any) error, into any) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := decode(text, into); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

func sorted(found []dependency) []dependency {
	slices.SortFunc(found, func(a, b dependency) int { return strings.Compare(a.name, b.name) })
	return found
}

func concrete(spec string) string {
	version := strings.TrimLeft(strings.TrimSpace(spec), "^~=<>!v ")
	if cut := strings.IndexAny(version, ", ;"); cut >= 0 {
		version = version[:cut]
	}
	return version
}
