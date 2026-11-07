package deps

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tofu/internal/models/llm/core"
	platformfs "tofu/internal/platform/fs"
	"tofu/internal/tools/contract"
)

const Name = "deps"

type mode string

const (
	add      mode = "add"
	upgrade  mode = "upgrade"
	why      mode = "why"
	outdated mode = "outdated"
)

type Limits struct {
	Request, Run      time.Duration
	Advisories, Lines int
}

type Deps struct {
	root   contract.Root
	limits Limits
	http   *http.Client
}

type saved struct {
	body    []byte
	mode    fs.FileMode
	existed bool
}

func New(dir string, limits Limits) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Deps{root: root, limits: limits, http: &http.Client{Timeout: limits.Request}}, err
}

func (Deps) Name() string { return Name }

func (Deps) Definition() core.Tool {
	return core.Tool{
		Name: Name,
		Description: "adds, upgrades, explains or lists the project's dependencies for go, npm, cargo and python (uv), and says what a change costs before it makes it. " +
			"use it instead of go get, npm install, cargo add or uv add through bash, which print hundreds of lines and never the license or the advisories. " +
			"add and upgrade read the registry (proxy.golang.org, registry.npmjs.org, crates.io, pypi.org), the license from deps.dev and the known advisories from osv.dev first, then run the package manager and report from, to, license, size, advisories and which manifest and lockfile changed. " +
			"a version with a known advisory is refused with the versions that fix it unless accept_advisories is true; a registry that does not answer is named and nothing is written. " +
			"why names what pulls a package in; outdated lists direct dependencies with a newer release and the advisories on the version in use",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode":              map[string]any{"type": "string", "enum": []string{string(add), string(upgrade), string(why), string(outdated)}},
				"package":           map[string]any{"type": "string", "description": "module path, npm name, crate or PyPI name; name@version is accepted"},
				"version":           map[string]any{"type": "string", "description": "left out, the newest stable release"},
				"ecosystem":         map[string]any{"type": "string", "enum": []string{goModules.String(), npm.String(), cargo.String(), python.String()}, "description": "only needed when the directory holds more than one kind of project"},
				"accept_advisories": map[string]any{"type": "boolean"},
			},
			"required": []string{"mode"},
		},
	}
}

func (d Deps) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args struct {
		Mode             mode   `json:"mode"`
		Package          string `json:"package"`
		Version          string `json:"version"`
		Ecosystem        string `json:"ecosystem"`
		AcceptAdvisories bool   `json:"accept_advisories"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("deps: arguments are not the expected shape: %w", err)
	}
	if at := strings.LastIndex(args.Package, "@"); at > 0 && args.Version == "" {
		args.Package, args.Version = args.Package[:at], args.Package[at+1:]
	}
	e, err := detect(string(d.root), args.Ecosystem)
	if err != nil {
		return refused(string(args.Mode), err.Error()), nil
	}
	var result contract.Result
	switch args.Mode {
	case add, upgrade:
		if args.Package == "" {
			return refused(string(args.Mode), "name the package"), nil
		}
		result = d.change(ctx, e, args.Mode, args.Package, args.Version, args.AcceptAdvisories)
	case why:
		if args.Package == "" {
			return refused(string(args.Mode), "name the package"), nil
		}
		command := e.why(args.Package)
		out, err := d.run(ctx, e, command)
		if err != nil {
			return refused("why "+args.Package, strings.Join(command, " ")+": "+err.Error()+"\n"+out), nil
		}
		result = contract.Result{Content: "deps why " + args.Package + ": " + strings.Join(command, " ") + "\n" + out}
	case outdated:
		result = d.outdated(ctx, e)
	default:
		return refused(string(args.Mode), "mode must be add, upgrade, why or outdated"), nil
	}
	result.Command = strings.TrimSpace(string(args.Mode) + " " + args.Package)
	return result, nil
}

func refused(asked, said string) contract.Result {
	return contract.Failure("deps " + asked + ": " + strings.TrimRight(said, "\n") + "\n")
}

func (d Deps) change(ctx context.Context, e ecosystem, m mode, pkg, version string, accept bool) contract.Result {
	asked := string(m) + " " + pkg
	unwritten := func(err error) contract.Result { return refused(asked, "wrote nothing, "+err.Error()) }
	if e == python && !contract.IsFile(filepath.Join(string(d.root), "pyproject.toml")) {
		return unwritten(errors.New("uv add writes pyproject.toml and this project has none. run uv init --bare first"))
	}
	direct, err := e.direct(ctx, string(d.root))
	if err != nil {
		return unwritten(err)
	}
	from := ""
	for _, dep := range direct {
		if strings.EqualFold(dep.name, pkg) {
			from = dep.version
		}
	}
	if m == upgrade && from == "" {
		return refused(asked, pkg+" is not a dependency in "+e.lockfiles()[0]+". use mode add")
	}
	goLine := ""
	if e == goModules && version == "" {
		if version, goLine, err = d.newestGoAllowed(ctx, pkg); err != nil {
			return unwritten(err)
		}
	}
	to, found, err := d.release(ctx, e, pkg, version)
	switch {
	case err != nil:
		return unwritten(err)
	case !found:
		return unwritten(errors.New(e.registry() + " has no " + pkg + "@" + cmp.Or(version, "latest")))
	case to.version == from:
		return contract.Result{Content: "deps " + asked + ": already at " + from + ", nothing to do\n"}
	}
	license, err := d.licensed(ctx, e, pkg, to)
	if err != nil {
		return unwritten(err)
	}
	queries := []dependency{{name: pkg, version: to.version}}
	var said strings.Builder
	fmt.Fprintf(&said, "deps %s\necosystem: %s\nfrom: %s\nto: %s\nlicense: %s\nsize: %d bytes (%s)\n%s", asked, e, cmp.Or(from, "none"), to.version, license, to.size, e.registry(), goLine)
	if from != "" {
		before, found, err := d.release(ctx, e, pkg, from)
		if err != nil {
			return unwritten(err)
		}
		if found {
			was, err := d.licensed(ctx, e, pkg, before)
			if err != nil {
				return unwritten(err)
			}
			fmt.Fprintf(&said, "size_delta: %+d bytes\nlicense_from: %s\n", to.size-before.size, was)
		}
		queries = append(queries, dependency{name: pkg, version: from})
	}
	ids, err := d.vulnerable(ctx, e, queries)
	if err != nil {
		return unwritten(err)
	}
	advisories, unread, err := d.described(ctx, pkg, ids[0])
	if err != nil {
		return unwritten(err)
	}
	fmt.Fprintf(&said, "advisories: %d on %s (osv.dev)\n", len(advisories)+len(unread), to.version)
	for _, a := range advisories {
		named := append([]string{a.id}, a.aliases...)
		if a.severity != "" {
			named = append(named, a.severity)
		}
		fmt.Fprintf(&said, "  %s: %s, fixed in %s\n", strings.Join(named, " "), a.summary, cmp.Or(strings.Join(a.fixed, " "), "no release yet"))
	}
	if len(unread) > 0 {
		fmt.Fprintf(&said, "  and %s, not read\n", strings.Join(unread, " "))
	}
	if len(ids) > 1 {
		fmt.Fprintf(&said, "advisories_from: %d on %s\n", len(ids[1]), from)
	}
	if len(ids[0]) > 0 && !accept {
		return contract.Failure(said.String() + "wrote nothing: " + to.version + " has known advisories. name a fixed version, or pass accept_advisories true to take it anyway\n")
	}
	return d.write(ctx, e, e.install(pkg, to.version), &said)
}

func (d Deps) write(ctx context.Context, e ecosystem, command []string, said *strings.Builder) contract.Result {
	dir := string(d.root)
	before := map[string]saved{}
	for _, name := range e.lockfiles() {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			before[name] = saved{}
			continue
		}
		body, readErr := os.ReadFile(path)
		if err := cmp.Or(err, readErr); err != nil {
			return contract.Failure(said.String() + "wrote nothing, " + err.Error() + "\n")
		}
		before[name] = saved{body: body, mode: info.Mode().Perm(), existed: true}
	}
	ran := strings.Join(command, " ")
	out, runErr := d.run(ctx, e, command)
	var changed []string
	for _, name := range e.lockfiles() {
		path := filepath.Join(dir, name)
		now, err := os.ReadFile(path)
		was := before[name]
		switch {
		case err == nil && !was.existed:
			changed = append(changed, name+" created")
		case err != nil && was.existed:
			changed = append(changed, name+" removed")
		case err == nil && !bytes.Equal(now, was.body):
			changed = append(changed, name+" changed")
		default:
			continue
		}
		if runErr == nil {
			continue
		}
		var restore error
		if was.existed {
			restore = platformfs.WriteFile(path, was.body, was.mode)
		} else {
			restore = os.Remove(path)
		}
		if restore != nil {
			changed[len(changed)-1] += " and not restored, " + restore.Error()
		}
	}
	if runErr != nil {
		return contract.Failure(fmt.Sprintf("%s%s failed, %v. put back as they were: %s\n%s", said, ran, runErr, contract.Listed(changed), out))
	}
	fmt.Fprintf(said, "ran: %s\nlockfile_changed: %s\n", ran, contract.Listed(changed))
	return contract.Result{Content: said.String()}
}

func (d Deps) run(ctx context.Context, e ecosystem, command []string) (string, error) {
	program, err := e.program()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, d.limits.Run)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, command[1:]...)
	cmd.Dir = string(d.root)
	out, err := cmd.CombinedOutput()
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > d.limits.Lines {
		lines = append(lines[:d.limits.Lines], fmt.Sprintf("and %d more lines", len(lines)-d.limits.Lines))
	}
	return strings.Join(lines, "\n") + "\n", err
}

func (d Deps) outdated(ctx context.Context, e ecosystem) contract.Result {
	listed, err := e.direct(ctx, string(d.root))
	if err != nil {
		return refused(string(outdated), err.Error())
	}
	var direct, pinned []dependency
	for _, dep := range listed {
		if dep.indirect {
			continue
		}
		direct = append(direct, dep)
		if dep.version != "" {
			pinned = append(pinned, dep)
		}
	}
	if len(direct) == 0 {
		return contract.Result{Content: fmt.Sprintf("deps outdated: no direct dependency in %s, %d marked indirect\n", e.lockfiles()[0], len(listed))}
	}
	advised := map[string][]string{}
	if len(pinned) > 0 {
		ids, err := d.vulnerable(ctx, e, pinned)
		if err != nil {
			return refused(string(outdated), err.Error())
		}
		for i, dep := range pinned {
			advised[dep.name] = ids[i]
		}
	}
	var said strings.Builder
	newer := 0
	for _, dep := range direct {
		latest, found, err := d.release(ctx, e, dep.name, "")
		switch {
		case err != nil:
			return refused(string(outdated), err.Error())
		case !found:
			fmt.Fprintf(&said, "%s %s: %s has no such package\n", dep.name, cmp.Or(dep.version, "unpinned"), e.registry())
		case latest.version == dep.version:
			fmt.Fprintf(&said, "%s %s: newest\n", dep.name, dep.version)
		default:
			newer++
			fmt.Fprintf(&said, "%s %s: %s is out\n", dep.name, cmp.Or(dep.version, "unpinned"), latest.version)
		}
		if ids := advised[dep.name]; len(ids) > 0 {
			fmt.Fprintf(&said, "  advisories on %s: %s\n", dep.version, strings.Join(ids, " "))
		}
	}
	return contract.Result{Content: fmt.Sprintf("deps outdated: %d of %d direct dependencies have a newer release (%s, osv.dev)\n", newer, len(direct), e.registry()) + said.String()}
}
