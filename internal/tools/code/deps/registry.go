package deps

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	goversion "go/version"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"tofu/internal/konst"
)

const (
	depsDev  = "https://api.deps.dev/v3/systems/"
	osvBatch = "https://api.osv.dev/v1/querybatch"
	osvVulns = "https://api.osv.dev/v1/vulns/"
)

type release struct {
	version, license string
	size             int64
}

type advisory struct {
	id, summary, severity string
	aliases, fixed        []string
}

func (d Deps) ask(ctx context.Context, method, address string, body []byte, into any) (bool, int64, error) {
	request, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(body))
	if err != nil {
		return false, 0, err
	}
	request.Header.Set("User-Agent", "tofu/"+konst.Version)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	host := request.URL.Host
	response, err := d.http.Do(request)
	if err != nil {
		return false, 0, fmt.Errorf("%s did not answer: %w", host, err)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone:
		return false, 0, nil
	case response.StatusCode != http.StatusOK:
		return false, 0, fmt.Errorf("%s answered %s for %s", host, response.Status, address)
	case into == nil:
		return true, response.ContentLength, nil
	}
	if raw, ok := into.(*[]byte); ok {
		*raw, err = io.ReadAll(response.Body)
		return err == nil, response.ContentLength, err
	}
	if err := json.NewDecoder(response.Body).Decode(into); err != nil {
		return false, 0, fmt.Errorf("%s sent an answer that is not the expected JSON: %w", host, err)
	}
	return true, response.ContentLength, nil
}

func (d Deps) release(ctx context.Context, e ecosystem, pkg, version string) (release, bool, error) {
	base := "https://" + e.registry() + "/"
	switch e {
	case goModules:
		module := base + goEscape(pkg)
		var info struct{ Version string }
		address := module + "/@latest"
		if version != "" {
			address = module + "/@v/" + goEscape(version) + ".info"
		}
		found, _, err := d.ask(ctx, http.MethodGet, address, nil, &info)
		if !found || err != nil {
			return release{}, found, err
		}
		_, size, err := d.ask(ctx, http.MethodHead, module+"/@v/"+goEscape(info.Version)+".zip", nil, nil)
		return release{version: info.Version, size: size}, true, err
	case npm:
		var doc struct {
			Version string
			License json.RawMessage
			Dist    struct{ UnpackedSize int64 }
		}
		found, _, err := d.ask(ctx, http.MethodGet, base+pkg+"/"+cmp.Or(version, "latest"), nil, &doc)
		var license string
		_ = json.Unmarshal(doc.License, &license)
		return release{version: doc.Version, license: license, size: doc.Dist.UnpackedSize}, found, err
	case cargo:
		crate := base + "api/v1/crates/" + url.PathEscape(pkg)
		if version == "" {
			var doc struct {
				Crate struct {
					MaxStableVersion string `json:"max_stable_version"`
				}
			}
			if found, _, err := d.ask(ctx, http.MethodGet, crate, nil, &doc); !found || err != nil {
				return release{}, found, err
			}
			version = doc.Crate.MaxStableVersion
		}
		var doc struct {
			Version struct {
				Num, License string
				CrateSize    int64 `json:"crate_size"`
			}
		}
		found, _, err := d.ask(ctx, http.MethodGet, crate+"/"+url.PathEscape(version), nil, &doc)
		return release{version: doc.Version.Num, license: doc.Version.License, size: doc.Version.CrateSize}, found, err
	case python:
		address := base + "pypi/" + url.PathEscape(pkg) + "/json"
		if version != "" {
			address = base + "pypi/" + url.PathEscape(pkg) + "/" + url.PathEscape(version) + "/json"
		}
		var doc struct {
			Info struct{ Version, License string }
			URLs []struct {
				Size        int64
				PackageType string `json:"packagetype"`
			} `json:"urls"`
		}
		found, _, err := d.ask(ctx, http.MethodGet, address, nil, &doc)
		got := release{version: doc.Info.Version, license: doc.Info.License}
		for _, file := range doc.URLs {
			if got.size == 0 || file.PackageType == "sdist" {
				got.size = file.Size
			}
		}
		return got, found, err
	}
	panic(fmt.Sprintf("deps: unknown ecosystem %d", int(e)))
}

func (d Deps) newestGoAllowed(ctx context.Context, pkg string) (string, string, error) {
	text, err := os.ReadFile(filepath.Join(string(d.root), "go.mod"))
	if err != nil {
		return "", "", err
	}
	project, err := modfile.ParseLax("go.mod", text, nil)
	if err != nil || project.Go == nil {
		return "", "", err
	}
	allowed := project.Go.Version
	module := "https://" + goModules.registry() + "/" + goEscape(pkg)
	var latest struct{ Version string }
	if found, _, err := d.ask(ctx, http.MethodGet, module+"/@latest", nil, &latest); !found || err != nil {
		return "", "", err
	}
	newestNeeds, err := d.goLine(ctx, module, latest.Version)
	if err != nil || goversion.Compare("go"+newestNeeds, "go"+allowed) <= 0 {
		return "", "", err
	}
	var listed []byte
	if _, _, err := d.ask(ctx, http.MethodGet, module+"/@v/list", nil, &listed); err != nil {
		return "", "", err
	}
	var older []string
	for _, v := range strings.Fields(string(listed)) {
		if semver.IsValid(v) && semver.Compare(v, latest.Version) < 0 && (semver.Prerelease(v) == "" || semver.Prerelease(latest.Version) != "") {
			older = append(older, v)
		}
	}
	semver.Sort(older)
	slices.Reverse(older)
	needs, errs := make([]string, len(older)), make([]error, len(older))
	var checked sync.WaitGroup
	for i, v := range older {
		checked.Go(func() { needs[i], errs[i] = d.goLine(ctx, module, v) })
	}
	checked.Wait()
	for i, v := range older {
		if errs[i] != nil {
			return "", "", errs[i]
		}
		if goversion.Compare("go"+needs[i], "go"+allowed) <= 0 {
			return v, fmt.Sprintf("go_line: kept at %s, the newest release %s needs go %s\n", allowed, latest.Version, newestNeeds), nil
		}
	}
	return "", "", fmt.Errorf("no version of %s allows this module's go %s. the newest, %s, needs go %s", pkg, allowed, latest.Version, newestNeeds)
}

func (d Deps) goLine(ctx context.Context, module, version string) (string, error) {
	var text []byte
	if _, _, err := d.ask(ctx, http.MethodGet, module+"/@v/"+goEscape(version)+".mod", nil, &text); err != nil {
		return "", err
	}
	mod, err := modfile.ParseLax(version+".mod", text, nil)
	if err != nil || mod.Go == nil {
		return "", err
	}
	return mod.Go.Version, nil
}

func (d Deps) licensed(ctx context.Context, e ecosystem, pkg string, got release) (string, error) {
	var doc struct{ Licenses []string }
	found, _, err := d.ask(ctx, http.MethodGet, depsDev+e.depsDev()+"/packages/"+url.PathEscape(pkg)+"/versions/"+url.PathEscape(got.version), nil, &doc)
	switch {
	case err != nil:
		return "", err
	case found && len(doc.Licenses) > 0:
		return strings.Join(doc.Licenses, ", ") + " (deps.dev)", nil
	case got.license != "":
		return got.license + " (" + e.registry() + ", not on deps.dev)", nil
	}
	return "unknown to deps.dev and " + e.registry(), nil
}

func (d Deps) vulnerable(ctx context.Context, e ecosystem, asked []dependency) ([][]string, error) {
	queries := make([]map[string]any, len(asked))
	for i, dep := range asked {
		queries[i] = map[string]any{"package": map[string]string{"name": dep.name, "ecosystem": e.osv()}, "version": dep.version}
	}
	body, err := json.Marshal(map[string]any{"queries": queries})
	if err != nil {
		return nil, err
	}
	var answer struct {
		Results []struct {
			Vulns []struct{ ID string }
		}
	}
	if _, _, err := d.ask(ctx, http.MethodPost, osvBatch, body, &answer); err != nil {
		return nil, err
	}
	if len(answer.Results) != len(asked) {
		return nil, fmt.Errorf("api.osv.dev answered %d results for %d queries", len(answer.Results), len(asked))
	}
	ids := make([][]string, len(asked))
	for i, result := range answer.Results {
		for _, vuln := range result.Vulns {
			ids[i] = append(ids[i], vuln.ID)
		}
	}
	return ids, nil
}

func (d Deps) described(ctx context.Context, pkg string, ids []string) ([]advisory, []string, error) {
	var shown []advisory
	var unread []string
	for _, id := range ids {
		if slices.ContainsFunc(shown, func(a advisory) bool { return a.id == id || slices.Contains(a.aliases, id) }) {
			continue
		}
		if len(shown) == d.limits.Advisories {
			unread = append(unread, id)
			continue
		}
		var doc struct {
			ID, Summary      string
			Aliases          []string
			DatabaseSpecific struct{ Severity string } `json:"database_specific"`
			Affected         []struct {
				Package struct{ Name string }
				Ranges  []struct {
					Events []struct{ Fixed string }
				}
			}
		}
		if _, _, err := d.ask(ctx, http.MethodGet, osvVulns+url.PathEscape(id), nil, &doc); err != nil {
			return nil, nil, err
		}
		got := advisory{id: doc.ID, summary: doc.Summary, severity: doc.DatabaseSpecific.Severity, aliases: doc.Aliases}
		for _, affected := range doc.Affected {
			if !strings.EqualFold(affected.Package.Name, pkg) {
				continue
			}
			for _, r := range affected.Ranges {
				for _, event := range r.Events {
					if event.Fixed != "" && !slices.Contains(got.fixed, event.Fixed) {
						got.fixed = append(got.fixed, event.Fixed)
					}
				}
			}
		}
		shown = append(shown, got)
	}
	return shown, unread, nil
}

func goEscape(path string) string {
	var escaped strings.Builder
	for _, r := range path {
		if 'A' <= r && r <= 'Z' {
			escaped.WriteString("!" + string(r+'a'-'A'))
			continue
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}
