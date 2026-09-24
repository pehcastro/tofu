package web_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"tofu/internal/web"
	"tofu/library"
)

const absentKey = "TOFU_TEST_SEARCH_KEY_NOBODY_SETS"

func shipped(t *testing.T) web.Layer {
	t.Helper()
	layers, err := web.DefaultLayers(library.Files())
	if err != nil {
		t.Fatalf("building the default layers: %v", err)
	}
	return layers[0]
}

func project(files map[string]string) web.Layer {
	mapped := fstest.MapFS{}
	for name, body := range files {
		mapped[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return web.Layer{Origin: "project", FS: mapped}
}

func TestTheLayersAreTheShippedLibraryThenTheHomeThenTheProject(t *testing.T) {
	layers, err := web.DefaultLayers(library.Files())
	if err != nil {
		t.Fatalf("building the default layers: %v", err)
	}
	var origins []string
	for _, layer := range layers {
		origins = append(origins, layer.Origin)
	}
	t.Logf("layers: %v", origins)
	if len(layers) != 3 {
		t.Fatalf("expected the library, the home and the project: %v", origins)
	}
	if !strings.HasSuffix(origins[0], filepath.Join("library", "web")) {
		t.Fatalf("the shipped library is not the first layer: %q", origins[0])
	}
	for _, origin := range origins[1:] {
		if !strings.HasSuffix(origin, "web") || origin == origins[0] {
			t.Fatalf("an override layer is not a web directory of its own: %q", origin)
		}
	}
	if origins[1] == origins[2] {
		t.Fatalf("the home layer and the project layer are the same directory: %q", origins[1])
	}
}

func writeFetch(t *testing.T, dir string, maxBytes string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fetch.yaml"), []byte("max_bytes: "+maxBytes+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
}

func TestTheProjectWebFileBeatsTheGlobalWhichBeatsTheShipped(t *testing.T) {
	found, err := web.DefaultLayers(library.Files())
	if err != nil {
		t.Fatalf("layers: %v", err)
	}
	names := []string{found[0].Name, found[1].Name, found[2].Name}
	t.Logf("layers %v origins %q %q %q", names, found[0].Origin, found[1].Origin, found[2].Origin)
	if names[0] != "library" || names[1] != "global" || names[2] != "project" {
		t.Fatalf("the stack is not library, global, project: %v", names)
	}
	for _, layer := range found {
		if filepath.Base(layer.Origin) != "web" {
			t.Fatalf("the %s layer is not a web directory: %q", layer.Name, layer.Origin)
		}
	}

	ceiling := func() int {
		config, err := web.Load(found)
		if err != nil {
			t.Fatalf("loading: %v", err)
		}
		if config.TimeoutMS != 20000 {
			t.Fatalf("an override of one field dropped the rest: %d ms", config.TimeoutMS)
		}
		return config.MaxPageBytes
	}

	if got := ceiling(); got != 5000000 {
		t.Fatalf("with nothing overriding it the shipped file lost: %d", got)
	}
	writeFetch(t, found[1].Origin, "4096")
	if got := ceiling(); got != 4096 {
		t.Fatalf("the global directory did not beat the shipped library: %d", got)
	}
	writeFetch(t, found[2].Origin, "512")
	if got := ceiling(); got != 512 {
		t.Fatalf("the project directory did not beat the global one: %d", got)
	}
}

func TestTheShippedLibraryResolvesFromAnyWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	layers, err := web.DefaultLayers(library.Files())
	if err != nil {
		t.Fatalf("building the default layers: %v", err)
	}
	config, err := web.Load(layers)
	t.Logf("library layer %q, provider %q, ceiling %d, err %v", layers[0].Origin, config.Provider.Name, config.MaxPageBytes, err)
	if err != nil {
		t.Fatalf("loading away from the repository root: %v", err)
	}
	if config.Provider.Name != "brave" || config.MaxPageBytes != 5000000 {
		t.Fatalf("the shipped library did not resolve away from the repository root: %+v", config)
	}
}

func TestTheShippedLibraryNamesTheProviderAndTheFetchCeiling(t *testing.T) {
	config, err := web.Load([]web.Layer{shipped(t)})
	if err != nil {
		t.Fatalf("loading the shipped library: %v", err)
	}
	t.Logf("provider %s from %s, ceiling %d bytes, timeout %d ms",
		config.Provider.Name, config.Provider.Origin, config.MaxPageBytes, config.TimeoutMS)
	if config.Provider.Name != "brave" || config.Provider.KeyHeader != "X-Subscription-Token" {
		t.Fatalf("the shipped provider is not brave: %+v", config.Provider)
	}
	if config.MaxPageBytes != 5000000 || config.TimeoutMS != 20000 {
		t.Fatalf("the shipped fetch limits changed: %d bytes, %d ms", config.MaxPageBytes, config.TimeoutMS)
	}
	if config.FetchUse != web.FetchUseOn || !config.HasFetch() {
		t.Fatalf("the shipped default is not fetch on: %q", config.FetchUse)
	}
}

func TestAProjectTurnsFetchOffWithoutTouchingSearch(t *testing.T) {
	t.Setenv(absentKey, "")
	config, err := web.Load([]web.Layer{shipped(t), project(map[string]string{
		"fetch.yaml":          "use: off\n",
		"search/searxng.yaml": "use: default\nendpoint: http://searx.example/search?format=json\n",
		"search/brave.yaml":   "use: excluded\n",
	})})
	if err != nil {
		t.Fatalf("loading with fetch off: %v", err)
	}
	t.Logf("use %q from %s, fetch %v, search %v", config.FetchUse, config.FetchOrigin, config.HasFetch(), config.HasSearch())
	if config.HasFetch() {
		t.Fatalf("a project saying use: off still reads as fetch on: %q", config.FetchUse)
	}
	if !config.HasSearch() {
		t.Fatal("turning fetch off took search with it")
	}
}

func TestAProjectOverridesTheProviderWithoutTouchingCode(t *testing.T) {
	t.Setenv(absentKey, "")
	config, err := web.Load([]web.Layer{shipped(t), project(map[string]string{
		"search/brave.yaml":   "use: excluded\n",
		"search/searxng.yaml": "use: default\nendpoint: http://searx.example/search?format=json\n",
		"fetch.yaml":          "max_bytes: 4096\n",
	})})
	if err != nil {
		t.Fatalf("loading with a project layer: %v", err)
	}
	t.Logf("provider %s endpoint %s from %s, ceiling %d", config.Provider.Name, config.Provider.Endpoint, config.Provider.Origin, config.MaxPageBytes)
	if config.Provider.Name != "searxng" || config.Provider.Endpoint != "http://searx.example/search?format=json" {
		t.Fatalf("the project layer did not choose the provider: %+v", config.Provider)
	}
	if config.MaxPageBytes != 4096 || config.TimeoutMS != 20000 {
		t.Fatalf("the project layer did not override one field and keep the rest: %+v", config)
	}
	if !config.HasSearch() {
		t.Fatal("a provider that needs no key is not available")
	}
}

func TestAProviderWhoseKeyIsMissingIsNotAvailable(t *testing.T) {
	layers := []web.Layer{shipped(t), project(map[string]string{
		"search/brave.yaml": "key_variable: " + absentKey + "\n",
	})}

	t.Setenv(absentKey, "")
	without, err := web.Load(layers)
	if err != nil {
		t.Fatalf("loading without the key: %v", err)
	}
	if without.HasSearch() {
		t.Fatal("a provider with no key reads as available")
	}

	t.Setenv(absentKey, "a-key-that-is-never-printed")
	with, err := web.Load(layers)
	if err != nil {
		t.Fatalf("loading with the key: %v", err)
	}
	if !with.HasSearch() {
		t.Fatal("a provider with a key reads as absent")
	}
}

func TestALibraryThatCannotBeTrustedIsRefusedRatherThanGuessed(t *testing.T) {
	for _, broken := range []struct {
		name  string
		files map[string]string
		says  string
	}{
		{
			name:  "two defaults",
			files: map[string]string{"search/searxng.yaml": "use: default\n"},
			says:  "both say use: default",
		},
		{
			name:  "an unknown field",
			files: map[string]string{"search/brave.yaml": "endpont: https://example.org\n"},
			says:  "is not a field of this file",
		},
		{
			name:  "a use nobody defined",
			files: map[string]string{"search/brave.yaml": "use: sometimes\n"},
			says:  "use has to be default, allowed or excluded",
		},
		{
			name:  "a fetch use nobody defined",
			files: map[string]string{"fetch.yaml": "use: sometimes\n"},
			says:  "use has to be on or off",
		},
		{
			name:  "a ceiling that is not a number",
			files: map[string]string{"fetch.yaml": "max_bytes: plenty\n"},
			says:  "max_bytes has to be a positive whole number",
		},
		{
			name:  "an endpoint that is not an address",
			files: map[string]string{"search/brave.yaml": "endpoint: search.example.org\n"},
			says:  "is not an http or https address",
		},
	} {
		t.Run(broken.name, func(t *testing.T) {
			_, err := web.Load([]web.Layer{shipped(t), project(broken.files)})
			t.Logf("%s: %v", broken.name, err)
			if err == nil || !strings.Contains(err.Error(), broken.says) {
				t.Fatalf("%s was not refused with %q: %v", broken.name, broken.says, err)
			}
		})
	}
}
