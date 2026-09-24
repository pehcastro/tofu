package web

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/sys"
)

type Layer = sys.Layer

func Layers(library fs.FS, dir string) ([]Layer, error) {
	shipped, err := fs.Sub(library, "web")
	if err != nil {
		return nil, err
	}
	return sys.Layers(shipped, "web", dir)
}

type Provider struct {
	Name         string
	Origin       string
	Endpoint     string
	QueryParam   string
	CountParam   string
	MaxResults   int
	KeyVariable  string
	KeyHeader    string
	KeyParam     string
	ResultsPath  string
	TitleField   string
	URLField     string
	SnippetField string
}

const (
	FetchUseOn  = "on"
	FetchUseOff = "off"
)

type Config struct {
	MaxPageBytes int
	TimeoutMS    int
	FetchUse     string
	FetchOrigin  string
	Provider     Provider
	searchKey    string
}

func (c Config) HasFetch() bool { return c.FetchUse != FetchUseOff }

func (c Config) HasSearch() bool {
	return c.Provider.Endpoint != "" && (c.Provider.KeyVariable == "" || c.searchKey != "")
}

var fetchFields = map[string]bool{"use": true, "max_bytes": true, "timeout_ms": true}

var providerFields = map[string]bool{
	"use": true, "reason": true, "endpoint": true, "query_param": true, "count_param": true,
	"max_results": true, "key_variable": true, "key_header": true, "key_param": true,
	"results_path": true, "title_field": true, "url_field": true, "snippet_field": true,
}

func Load(layers []Layer) (Config, error) {
	fetch := sheet{values: map[string]string{}}
	providers := map[string]*sheet{}
	for _, layer := range layers {
		if err := fetch.read(layer, "fetch.yaml", fetchFields); err != nil {
			return Config{}, err
		}
		entries, err := fs.ReadDir(layer.FS, "search")
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name, isYAML := strings.CutSuffix(entry.Name(), ".yaml")
			if entry.IsDir() || !isYAML {
				return Config{}, fmt.Errorf("web: %s holds one yaml file per search provider and nothing else, found %s",
					sys.Join(layer.Origin, "search"), entry.Name())
			}
			if providers[name] == nil {
				providers[name] = &sheet{values: map[string]string{}}
			}
			if err := providers[name].read(layer, "search/"+entry.Name(), providerFields); err != nil {
				return Config{}, err
			}
		}
	}

	config := Config{FetchUse: fetch.values["use"], FetchOrigin: fetch.file}
	if config.FetchUse != FetchUseOn && config.FetchUse != FetchUseOff {
		return Config{}, fmt.Errorf("web: %s: use has to be %s or %s, found %q",
			fetch.file, FetchUseOn, FetchUseOff, config.FetchUse)
	}
	var err error
	if config.MaxPageBytes, err = fetch.number("max_bytes"); err != nil {
		return Config{}, err
	}
	if config.TimeoutMS, err = fetch.number("timeout_ms"); err != nil {
		return Config{}, err
	}
	for _, name := range slices.Sorted(maps.Keys(providers)) {
		chosen := providers[name]
		switch chosen.values["use"] {
		case "default":
		case "allowed", "excluded":
			continue
		default:
			return Config{}, fmt.Errorf("web: %s: use has to be default, allowed or excluded, found %q",
				chosen.file, chosen.values["use"])
		}
		if config.Provider.Name != "" {
			return Config{}, fmt.Errorf("web: %s and %s both say use: default, and one search provider is used at a time",
				config.Provider.Origin, chosen.file)
		}
		if config.Provider, err = chosen.provider(name); err != nil {
			return Config{}, err
		}
	}
	if config.Provider.KeyVariable != "" {
		config.searchKey, _ = credential(config.Provider.KeyVariable)
	}
	return config, nil
}

type sheet struct {
	values map[string]string
	file   string
}

func (s *sheet) read(layer Layer, name string, allowed map[string]bool) error {
	body, err := fs.ReadFile(layer.FS, name)
	if err != nil {
		return nil
	}
	at := sys.Join(layer.Origin, name)
	for i, raw := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, split := strings.Cut(line, ":")
		if key = strings.TrimSpace(key); !split {
			return fmt.Errorf("web: %s line %d expects key: value, found %q", at, i+1, line)
		}
		if !allowed[key] {
			return fmt.Errorf("web: %s line %d: %s is not a field of this file", at, i+1, key)
		}
		s.values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	s.file = at
	return nil
}

func (s *sheet) number(field string) (int, error) {
	value, err := strconv.Atoi(s.values[field])
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("web: %s: %s has to be a positive whole number, found %q", s.file, field, s.values[field])
	}
	return value, nil
}

func (s *sheet) provider(name string) (Provider, error) {
	built := Provider{
		Name:         name,
		Origin:       s.file,
		Endpoint:     s.values["endpoint"],
		QueryParam:   s.values["query_param"],
		CountParam:   s.values["count_param"],
		KeyVariable:  s.values["key_variable"],
		KeyHeader:    s.values["key_header"],
		KeyParam:     s.values["key_param"],
		ResultsPath:  s.values["results_path"],
		TitleField:   s.values["title_field"],
		URLField:     s.values["url_field"],
		SnippetField: s.values["snippet_field"],
	}
	required := map[string]string{
		"endpoint": built.Endpoint, "query_param": built.QueryParam, "results_path": built.ResultsPath,
		"title_field": built.TitleField, "url_field": built.URLField, "snippet_field": built.SnippetField,
	}
	for _, field := range slices.Sorted(maps.Keys(required)) {
		if required[field] == "" {
			return Provider{}, fmt.Errorf("web: %s: %s is required of a search provider", s.file, field)
		}
	}
	if !strings.HasPrefix(built.Endpoint, "http://") && !strings.HasPrefix(built.Endpoint, "https://") {
		return Provider{}, fmt.Errorf("web: %s: endpoint %q is not an http or https address", s.file, built.Endpoint)
	}
	count, err := s.number("max_results")
	if err != nil {
		return Provider{}, err
	}
	built.MaxResults = count
	return built, nil
}

func credential(variable string) (string, bool) {
	if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
		return value, true
	}
	paths := []string{sys.CredentialFileName}
	if home, err := sys.HomeConfigDir(); err == nil {
		paths = append(paths, sys.Join(home, sys.CredentialFileName))
	}
	for _, path := range paths {
		raw, err := sys.ReadCredential(path)
		if err != nil {
			continue
		}
		if value := sys.CredentialAssignment(string(raw), variable); value != "" {
			return value, true
		}
	}
	return "", false
}
