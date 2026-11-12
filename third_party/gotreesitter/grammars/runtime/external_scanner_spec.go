package grammarruntime

import (
	"slices"
	"strings"
	"sync"
)

type ExternalScannerSourceFile struct {
	Path   string
	SHA256 string
}

type ExternalScannerSpec struct {
	Language       string
	UpstreamRepo   string
	UpstreamCommit string
	SourceFiles    []ExternalScannerSourceFile
	Externals      []string
}

var externalScannerSpecRegistry struct {
	sync.RWMutex
	registered []ExternalScannerSpec
}

func RegisterExternalScannerSpec(spec ExternalScannerSpec) {
	externalScannerSpecRegistry.Lock()
	defer externalScannerSpecRegistry.Unlock()
	externalScannerSpecRegistry.registered = append(externalScannerSpecRegistry.registered, spec)
}

func LookupExternalScannerSpec(name string) (ExternalScannerSpec, bool) {
	name = normalizeExternalScannerSpecName(name)
	if name == "" {
		return ExternalScannerSpec{}, false
	}
	externalScannerSpecRegistry.RLock()
	defer externalScannerSpecRegistry.RUnlock()
	registered := externalScannerSpecRegistry.registered
	for i := len(registered) - 1; i >= 0; i-- {
		if spec := registered[i]; normalizeExternalScannerSpecName(spec.Language) == name {
			return cloneExternalScannerSpec(spec, name), true
		}
	}
	return ExternalScannerSpec{}, false
}

func ExternalScannerSpecs() []ExternalScannerSpec {
	externalScannerSpecRegistry.RLock()
	defer externalScannerSpecRegistry.RUnlock()
	var out []ExternalScannerSpec
	registered := externalScannerSpecRegistry.registered
	for i := len(registered) - 1; i >= 0; i-- {
		spec := registered[i]
		name := normalizeExternalScannerSpecName(spec.Language)
		if name != "" && !slices.ContainsFunc(out, func(kept ExternalScannerSpec) bool { return kept.Language == name }) {
			out = append(out, cloneExternalScannerSpec(spec, name))
		}
	}
	slices.SortFunc(out, func(a, b ExternalScannerSpec) int { return strings.Compare(a.Language, b.Language) })
	return out
}

func cloneExternalScannerSpec(spec ExternalScannerSpec, name string) ExternalScannerSpec {
	spec.Language = name
	spec.SourceFiles = slices.Clone(spec.SourceFiles)
	spec.Externals = slices.Clone(spec.Externals)
	return spec
}

func normalizeExternalScannerSpecName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
