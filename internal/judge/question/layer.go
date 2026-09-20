package question

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

type Layer struct {
	Name   string
	Origin string
	FS     fs.FS
}

func DirLayer(name string, dir string) Layer {
	return Layer{Name: name, Origin: dir, FS: os.DirFS(dir)}
}

func DefaultLayers(catalog fs.FS) ([]Layer, error) {
	dir, err := sys.CatalogDir()
	if err != nil {
		return nil, err
	}
	shipped := DirLayer("catalog", sys.Join(dir, "questions"))
	if catalog != nil {
		shipped = Layer{Name: "catalog", Origin: "catalog/questions", FS: catalog}
	}
	home, err := sys.HomeConfigDir()
	if err != nil {
		return nil, err
	}
	project, err := sys.ProjectStateDir()
	if err != nil {
		return nil, err
	}
	return []Layer{
		shipped,
		DirLayer("global", sys.Join(home, "questions")),
		DirLayer("project", sys.Join(project, "questions")),
	}, nil
}

type Field struct {
	Path  string
	Value string
	File  string
	Line  int
}

func Resolve(name string, layers []Layer) (Set, []Field, error) {
	base, want, exact := splitRequestedVersion(name)
	type hit struct {
		display string
		data    []byte
		version int
	}
	var hits []hit
	best := -1
	for _, layer := range layers {
		ms := matches(base, layer)
		if len(ms) == 0 {
			continue
		}
		if !exact && len(ms) > 1 {
			return Set{}, nil, ambiguityError(base, layer, ms)
		}
		m := ms[0]
		if exact {
			var found bool
			for _, mm := range ms {
				if mm.version == want {
					m, found = mm, true
					break
				}
			}
			if !found {
				continue
			}
		}
		data, err := fs.ReadFile(layer.FS, m.file)
		if err != nil {
			return Set{}, nil, err
		}
		hits = append(hits, hit{display: sys.Join(layer.Origin, m.file), data: data, version: m.version})
		if m.version > best {
			best = m.version
		}
	}
	if best < 0 {
		if exact {
			return Set{}, nil, fmt.Errorf("no question set named %q at version %d in any layer", base, want)
		}
		return Set{}, nil, fmt.Errorf("no question set named %q in any layer", base)
	}
	var merged *node
	var file string
	for _, h := range hits {
		if h.version != best {
			continue
		}
		root, err := parse(h.display, h.data)
		if err != nil {
			return Set{}, nil, err
		}
		merged = merge(merged, root)
		file = h.display
	}
	set, err := decode(merged, base, best, file)
	if err != nil {
		return Set{}, nil, err
	}
	return set, fieldsOf("", merged, nil), nil
}

func ambiguityError(name string, layer Layer, ms []fileMatch) error {
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = fmt.Sprintf("%s@%d (%s)", name, m.version, sys.Join(layer.Origin, m.file))
	}
	return fmt.Errorf("%q is ambiguous in the %s layer: %s", name, layer.Name, strings.Join(parts, ", "))
}

func splitRequestedVersion(name string) (string, int, bool) {
	base, version, ok := splitFileName(name + ".yaml")
	if !ok {
		return name, 0, false
	}
	return base, version, true
}

type fileMatch struct {
	file    string
	version int
}

func matches(name string, layer Layer) []fileMatch {
	if layer.FS == nil {
		return nil
	}
	entries, err := fs.ReadDir(layer.FS, ".")
	if err != nil {
		return nil
	}
	var out []fileMatch
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n, v, ok := splitFileName(e.Name())
		if !ok || n != name {
			continue
		}
		out = append(out, fileMatch{file: e.Name(), version: v})
	}
	return out
}

func splitFileName(base string) (string, int, bool) {
	if !strings.HasSuffix(base, ".yaml") {
		return "", 0, false
	}
	stem := strings.TrimSuffix(base, ".yaml")
	at := strings.LastIndexByte(stem, '@')
	if at <= 0 {
		return "", 0, false
	}
	version, err := strconv.Atoi(stem[at+1:])
	if err != nil {
		return "", 0, false
	}
	return stem[:at], version, true
}

func merge(base, over *node) *node {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}
	if base.kind != nodeMap || over.kind != nodeMap {
		return over
	}
	m := &node{kind: nodeMap, fields: map[string]*node{}, file: over.file, line: over.line}
	for _, k := range base.keys {
		m.set(k, base.fields[k])
	}
	for _, k := range over.keys {
		if b, ok := base.fields[k]; ok {
			m.set(k, merge(b, over.fields[k]))
			continue
		}
		m.set(k, over.fields[k])
	}
	return m
}

func fieldsOf(prefix string, n *node, out []Field) []Field {
	if n == nil {
		return out
	}
	switch n.kind {
	case nodeScalar:
		return append(out, Field{Path: prefix, Value: n.text, File: n.file, Line: n.line})
	case nodeMap:
		for _, k := range n.keys {
			out = fieldsOf(join(prefix, k), n.fields[k], out)
		}
		return out
	case nodeList:
		for i, item := range n.items {
			out = fieldsOf(fmt.Sprintf("%s[%d]", prefix, i), item, out)
		}
		return out
	}
	return out
}

func join(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
