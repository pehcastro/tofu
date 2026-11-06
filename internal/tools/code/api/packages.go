package api

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

func tsInstalled(from, pkg string) (installed, error) {
	root, ok := upward(from, filepath.Join("node_modules", filepath.FromSlash(pkg), "package.json"))
	if !ok {
		return installed{}, fmt.Errorf("%s is not in node_modules at %s or above it. install it with: npm install %s", pkg, from, pkg)
	}
	dir := filepath.Join(root, "node_modules", filepath.FromSlash(pkg))
	var manifest struct{ Version, Types, Typings string }
	if err := readJSON(filepath.Join(dir, "package.json"), &manifest); err != nil {
		return installed{}, err
	}
	entry := filepath.Join(dir, filepath.FromSlash(cmp.Or(manifest.Types, manifest.Typings)))
	inst := installed{version: manifest.Version, source: "node_modules", dir: dir, files: entryFirst(sourceFiles(dir, ".d.ts", ".d.mts", ".d.cts"), entry)}
	if len(inst.files) > 0 {
		return inst, nil
	}
	types := "@types/" + strings.Replace(strings.TrimPrefix(pkg, "@"), "/", "__", 1)
	typesRoot, ok := upward(from, filepath.Join("node_modules", filepath.FromSlash(types), "package.json"))
	if !ok {
		return installed{}, fmt.Errorf("%s %s ships no .d.ts and %s is not installed. install it with: npm install -D %s", pkg, manifest.Version, types, types)
	}
	inst.dir = filepath.Join(typesRoot, "node_modules", filepath.FromSlash(types))
	var typesManifest struct{ Version string }
	if err := readJSON(filepath.Join(inst.dir, "package.json"), &typesManifest); err != nil {
		return installed{}, err
	}
	inst.source = "node_modules, types from " + types + " " + typesManifest.Version
	inst.files = sourceFiles(inst.dir, ".d.ts", ".d.mts", ".d.cts")
	return inst, nil
}

func rustInstalled(from, pkg string) (installed, error) {
	root, ok := upward(from, "Cargo.lock")
	if !ok {
		return installed{}, fmt.Errorf("no Cargo.lock in %s or above it, so no version is pinned. cargo generate-lockfile pins them, then install with: cargo fetch", from)
	}
	var lock struct {
		Package []struct{ Name, Version, Source string } `toml:"package"`
	}
	lockPath := filepath.Join(root, "Cargo.lock")
	text, err := os.ReadFile(lockPath)
	if err != nil {
		return installed{}, err
	}
	if err := toml.Unmarshal(text, &lock); err != nil {
		return installed{}, fmt.Errorf("%s: %w", lockPath, err)
	}
	crate, name := strings.ReplaceAll(pkg, "-", "_"), pkg
	var locked []string
	for _, p := range lock.Package {
		if strings.ReplaceAll(p.Name, "-", "_") == crate && strings.HasPrefix(p.Source, "registry+") {
			name, locked = p.Name, append(locked, p.Version)
		}
	}
	if len(locked) == 0 {
		return installed{}, fmt.Errorf("%s is not a registry crate in %s. install it with: cargo add %s", pkg, lockPath, pkg)
	}
	version := locked[len(locked)-1]
	home, _ := os.UserHomeDir()
	registry := filepath.Join(cmp.Or(os.Getenv("CARGO_HOME"), filepath.Join(home, ".cargo")), "registry", "src")
	dirs, _ := filepath.Glob(filepath.Join(registry, "*", name+"-"+version))
	if len(dirs) == 0 {
		return installed{}, fmt.Errorf("%s %s is locked but not in the cargo registry %s. install it with: cargo fetch", name, version, registry)
	}
	source := "cargo registry"
	if len(locked) > 1 {
		source = fmt.Sprintf("cargo registry, the newest of %s locked", strings.Join(locked, ", "))
	}
	src := filepath.Join(dirs[0], "src")
	return installed{version: version, source: source, dir: dirs[0], files: entryFirst(sourceFiles(src, ".rs"), filepath.Join(src, "lib.rs"))}, nil
}

func pythonInstalled(ctx context.Context, from, pkg string) (installed, error) {
	var sites []string
	if venv, ok := upward(from, ".venv"); ok {
		for _, pattern := range []string{"Lib/site-packages", "lib/python*/site-packages"} {
			found, _ := filepath.Glob(filepath.Join(venv, ".venv", filepath.FromSlash(pattern)))
			sites = append(sites, found...)
		}
	}
	for _, python := range []string{"python3", "python"} {
		if len(sites) > 0 {
			break
		}
		out, err := exec.CommandContext(ctx, python, "-c", "import site, sysconfig; print(sysconfig.get_paths()['purelib']); print(site.getusersitepackages())").Output()
		if err == nil {
			sites = strings.Split(strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n")), "\n")
		}
	}
	normal := func(name string) string {
		return strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(name))
	}
	for _, site := range sites {
		dists, _ := filepath.Glob(filepath.Join(site, "*.dist-info"))
		for _, dist := range dists {
			name, version, _ := strings.Cut(strings.TrimSuffix(filepath.Base(dist), ".dist-info"), "-")
			text, _ := os.ReadFile(filepath.Join(dist, "top_level.txt"))
			modules := strings.Fields(string(text))
			if normal(name) != normal(pkg) && !slices.Contains(modules, pkg) {
				continue
			}
			if len(modules) == 0 {
				modules = []string{normal(name)}
			}
			var files []string
			for _, module := range modules {
				files = append(files, sourceFiles(filepath.Join(site, module), ".py", ".pyi")...)
				for _, single := range []string{module + ".py", module + ".pyi"} {
					if info, err := os.Stat(filepath.Join(site, single)); err == nil && !info.IsDir() {
						files = append(files, filepath.Join(site, single))
					}
				}
			}
			return installed{version: version, source: "site-packages", dir: site, files: files}, nil
		}
	}
	install := "pip install " + pkg
	if _, ok := upward(from, "uv.lock"); ok {
		install = "uv add " + pkg
	}
	return installed{}, fmt.Errorf("%s is not installed in the site-packages %v. install it with: %s", pkg, sites, install)
}

func sourceFiles(dir string, suffixes ...string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case entry.IsDir() && entry.Name() == "node_modules" && path != dir:
			return filepath.SkipDir
		case !entry.IsDir() && slices.ContainsFunc(suffixes, func(suffix string) bool { return strings.HasSuffix(entry.Name(), suffix) }):
			files = append(files, path)
		}
		return nil
	})
	return files
}

func entryFirst(files []string, entry string) []string {
	if !slices.Contains(files, entry) {
		return files
	}
	return append([]string{entry}, slices.DeleteFunc(files, func(path string) bool { return path == entry })...)
}

func readJSON(path string, into any) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(text, into); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
