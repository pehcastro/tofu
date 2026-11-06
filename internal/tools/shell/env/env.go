package env

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"tofu/internal/konst"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
)

const manifestsLookedFor = "go.mod, package.json, rust-toolchain.toml, Cargo.toml, pyproject.toml, requirements*.txt, setup.py"

type declared struct {
	name, source string
	want, floor  string
	wantErr      error
	binaries     []string
	versionArgs  []string
	probeEnv     []string
}

type Env struct{ root contract.Root }

func New(dir string) (contract.Tool, error) {
	root, err := contract.NewRoot(dir)
	return Env{root: root}, err
}

func (Env) Name() string { return "env" }

func (Env) Definition() core.Tool {
	return core.Tool{
		Name: "env",
		Description: "says whether this project can build on this machine and what is missing, before the first build command fails. " +
			"reads the toolchains the project declares (go.mod, package.json engines.node, rust-toolchain.toml or Cargo.toml rust-version, pyproject.toml requires-python, requirements*.txt), " +
			"and for each the version wanted and the version found on PATH, or missing, or unknown when the binary is there but its version cannot be read. " +
			"then the keys .env.example names, and for each whether .env or the environment sets it. it never prints a value",
		Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
	}
}

func (e Env) Run(ctx context.Context, _ json.RawMessage) (contract.Result, error) {
	dir := string(e.root)
	var blockers, unknowns []string
	var toolchains strings.Builder
	chains := declaredToolchains(dir)
	if len(chains) == 0 {
		toolchains.WriteString("toolchains: none declared, looked for " + manifestsLookedFor + "\n")
	} else {
		toolchains.WriteString("toolchains:\n")
	}
	for _, chain := range chains {
		want := cmp.Or(chain.want, "any")
		if chain.wantErr != nil {
			want = "unknown (" + chain.wantErr.Error() + ")"
		}
		fmt.Fprintf(&toolchains, "%s: want %s from %s, ", chain.name, want, chain.source)
		have, found, located := probe(ctx, dir, chain)
		switch {
		case !located:
			toolchains.WriteString("missing: not on PATH, looked for " + strings.Join(chain.binaries, ", ") + "\n")
			blockers = append(blockers, chain.name+" missing")
		case !found:
			toolchains.WriteString("have unknown: on PATH but its version could not be read\n")
			unknowns = append(unknowns, chain.name)
		case chain.floor != "" && olderThan(have, chain.floor):
			toolchains.WriteString("have " + have + ", older than wanted\n")
			blockers = append(blockers, chain.name+" "+have+" older than "+chain.floor)
		default:
			toolchains.WriteString("have " + have + "\n")
		}
	}
	keys, unset, total, err := envKeys(dir)
	if err != nil {
		return contract.Result{}, fmt.Errorf("env: %w", err)
	}
	if unset > 0 {
		blockers = append(blockers, fmt.Sprintf("%d of %d env keys not set", unset, total))
	}
	verdict := "ready: every declared toolchain found and every env key set"
	switch {
	case len(blockers) > 0:
		verdict = "not ready: " + strings.Join(blockers, ", ")
	case len(unknowns) > 0:
		verdict = "ready as far as can be read"
	case len(chains) == 0:
		verdict = "no toolchain declared; every env key set"
	}
	if len(unknowns) > 0 {
		verdict += "; version unknown: " + strings.Join(unknowns, ", ")
	}
	return contract.Result{Content: verdict + "\n" + toolchains.String() + keys}, nil
}

func declaredToolchains(dir string) []declared {
	var chains []declared
	if body, ok, err := readManifest(dir, "go.mod"); ok {
		want := goDirective(body)
		chains = append(chains, declared{name: "go", source: "go.mod", want: want, floor: want, wantErr: err,
			binaries: []string{"go"}, versionArgs: []string{"env", "GOVERSION"}, probeEnv: []string{"GO111MODULE=off", "GOTOOLCHAIN=local"}})
	}
	if body, ok, err := readManifest(dir, "package.json"); ok {
		var manifest struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
		}
		err = cmp.Or(err, json.Unmarshal(body, &manifest))
		chains = append(chains, declared{name: "node", source: "package.json", want: manifest.Engines.Node, floor: minimumOf(manifest.Engines.Node), wantErr: err,
			binaries: []string{"node"}, versionArgs: []string{"--version"}})
	}
	rust := declared{name: "rust", binaries: []string{"rustc"}, versionArgs: []string{"--version"}, probeEnv: []string{"RUSTUP_AUTO_INSTALL=0"}}
	if body, ok, err := readManifest(dir, "rust-toolchain.toml"); ok {
		var pin struct {
			Toolchain struct {
				Channel string `toml:"channel"`
			} `toml:"toolchain"`
		}
		rust.source, rust.wantErr = "rust-toolchain.toml", cmp.Or(err, toml.Unmarshal(body, &pin))
		rust.want = pin.Toolchain.Channel
	} else if body, ok, err := readManifest(dir, "Cargo.toml"); ok {
		var cargo struct {
			Package struct {
				RustVersion string `toml:"rust-version"`
			} `toml:"package"`
		}
		rust.source, rust.wantErr = "Cargo.toml", cmp.Or(err, toml.Unmarshal(body, &cargo))
		rust.want, rust.floor = cargo.Package.RustVersion, cargo.Package.RustVersion
	}
	python := declared{name: "python", binaries: []string{"python3", "python"}, versionArgs: []string{"--version"}}
	requirements, _ := filepath.Glob(filepath.Join(dir, "requirements*.txt"))
	if body, ok, err := readManifest(dir, "pyproject.toml"); ok {
		var project struct {
			Project struct {
				RequiresPython string `toml:"requires-python"`
			} `toml:"project"`
		}
		python.source, python.wantErr = "pyproject.toml", cmp.Or(err, toml.Unmarshal(body, &project))
		python.want, python.floor = project.Project.RequiresPython, minimumOf(project.Project.RequiresPython)
	} else if len(requirements) > 0 {
		python.source = filepath.Base(requirements[0])
	} else if contract.IsFile(filepath.Join(dir, "setup.py")) {
		python.source = "setup.py"
	}
	for _, chain := range []declared{rust, python} {
		if chain.source != "" {
			chains = append(chains, chain)
		}
	}
	return chains
}

func readManifest(dir, name string) ([]byte, bool, error) {
	file, err := os.Open(filepath.Join(dir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = file.Close() }()
	body, err := io.ReadAll(io.LimitReader(file, konst.EnvManifestBytes+1))
	if err == nil && len(body) > konst.EnvManifestBytes {
		err = fmt.Errorf("%s is larger than %d bytes", name, konst.EnvManifestBytes)
	}
	return body, true, err
}

func goDirective(body []byte) string {
	for line := range strings.Lines(string(body)) {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	return ""
}

func minimumOf(want string) string {
	if strings.Contains(want, "||") {
		return ""
	}
	for _, part := range strings.FieldsFunc(want, func(c rune) bool { return c == ',' || c == ' ' }) {
		if floor, ok := strings.CutPrefix(part, ">="); ok && firstVersion(floor) == floor {
			return floor
		}
	}
	return ""
}

func probe(ctx context.Context, dir string, chain declared) (have string, found, located bool) {
	for _, binary := range chain.binaries {
		path, err := exec.LookPath(binary)
		if err != nil {
			continue
		}
		located = true
		run, cancel := context.WithTimeout(ctx, konst.EnvProbeTimeoutMillis*time.Millisecond)
		command := exec.CommandContext(run, path, chain.versionArgs...)
		command.Dir, command.Env = dir, append(os.Environ(), chain.probeEnv...)
		out, err := command.Output()
		cancel()
		if have := firstVersion(string(out)); err == nil && have != "" {
			return have, true, true
		}
	}
	return "", false, located
}

func firstVersion(text string) string {
	for _, field := range strings.FieldsFunc(text, func(c rune) bool { return c != '.' && (c < '0' || c > '9') }) {
		if field = strings.Trim(field, "."); strings.Contains(field, ".") && !strings.Contains(field, "..") {
			return field
		}
	}
	return ""
}

func olderThan(have, floor string) bool {
	haveParts, floorParts := strings.Split(have, "."), strings.Split(floor, ".")
	for i := range max(len(haveParts), len(floorParts)) {
		h, f := versionPart(haveParts, i), versionPart(floorParts, i)
		if h != f {
			return h < f
		}
	}
	return false
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

func envKeys(dir string) (string, int, int, error) {
	example, ok, err := readManifest(dir, ".env.example")
	if !ok {
		return "env keys: no .env.example\n", 0, 0, nil
	}
	if err != nil {
		return "", 0, 0, err
	}
	dotenv, hasDotenv, err := readManifest(dir, ".env")
	if err != nil {
		return "", 0, 0, err
	}
	setInDotenv := map[string]bool{}
	for key, value := range assignments(dotenv) {
		setInDotenv[key] = setInDotenv[key] || value != ""
	}
	var report strings.Builder
	report.WriteString("env keys from .env.example:\n")
	seen, unset := map[string]bool{}, 0
	for key := range assignments(example) {
		if seen[key] {
			continue
		}
		seen[key] = true
		state := "set in .env"
		inDotenv, listed := setInDotenv[key]
		switch {
		case inDotenv:
		case os.Getenv(key) != "":
			state = "set in the environment"
		case listed:
			state, unset = "empty in .env", unset+1
		case !hasDotenv:
			state, unset = "missing, there is no .env", unset+1
		default:
			state, unset = "missing from .env", unset+1
		}
		if len(seen) <= konst.EnvKeysShown {
			report.WriteString(key + ": " + state + "\n")
		}
	}
	if len(seen) > konst.EnvKeysShown {
		fmt.Fprintf(&report, "%d more keys not shown\n", len(seen)-konst.EnvKeysShown)
	}
	return report.String(), unset, len(seen), nil
}

func assignments(body []byte) func(yield func(key, value string) bool) {
	return func(yield func(key, value string) bool) {
		for line := range strings.Lines(string(body)) {
			line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
			key, value, _ := strings.Cut(line, "=")
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if key == "" || strings.HasPrefix(key, "#") || strings.ContainsAny(key, " \t") {
				continue
			}
			if strings.HasPrefix(value, "#") {
				value = ""
			}
			if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
				value = value[1 : len(value)-1]
			}
			if !yield(key, strings.TrimSpace(value)) {
				return
			}
		}
	}
}
