package eval

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"tofu/internal/tools/contract"
)

const (
	goMainDir     = "tofueval"
	rustBinName   = "tofu_eval"
	generatedLead = `(?:[A-Za-z]:)?[^\s:"']*`
)

func goProgram(ctx context.Context, root, from string, req request, marker string) (program, error) {
	pkgDir := dirOf(from)
	module, found := upTo(root, pkgDir, "go.mod")
	if !found {
		return program{}, fmt.Errorf("no go.mod is at %s or above it inside the project", pkgDir)
	}
	modulePath, err := modulePathOf(filepath.Join(module, "go.mod"))
	if err != nil {
		return program{}, err
	}
	imports := map[string]string{}
	for _, path := range req.Imports {
		imports[path] = ""
	}
	code := req.Setup + "\n" + req.Expr
	if name := packageNameOf(pkgDir); name != "" && name != "main" && strings.Contains(code, name+".") {
		rel, _ := filepath.Rel(module, pkgDir)
		imports[strings.TrimSuffix(modulePath+"/"+filepath.ToSlash(rel), "/.")] = name
	}
	for _, std := range standardPackagesNamed(ctx, code, imports) {
		imports[std] = ""
	}
	var src strings.Builder
	src.WriteString("package main\n\nimport (\n\ttofuevaljson \"encoding/json\"\n\ttofuevalfmt \"fmt\"\n\ttofuevaltime \"time\"\n")
	for _, path := range slices.Sorted(maps.Keys(imports)) {
		fmt.Fprintf(&src, "\t%s %s\n", imports[path], strconv.Quote(path))
	}
	src.WriteString(")\n\nfunc main() {\n")
	source := generated{file: regexp.MustCompile(generatedLead + goMainDir + `[\\/]main\.go:(\d+):(\d+)`)}
	source.write(&src, req, "\ttofuEvalStarted := tofuevaltime.Now()\n", "\ttofuEvalValue := ")
	src.WriteString("\n" +
		"\ttofuEvalMs := float64(tofuevaltime.Since(tofuEvalStarted).Nanoseconds()) / 1e6\n" +
		"\ttofuEvalOut, _ := tofuevaljson.Marshal(map[string]any{\"value\": tofuevalfmt.Sprintf(\"%#v\", tofuEvalValue), \"type\": tofuevalfmt.Sprintf(\"%T\", tofuEvalValue), \"ms\": tofuEvalMs})\n" +
		"\ttofuevalfmt.Printf(\"\\n%s\\n%s\\n\", " + strconv.Quote(marker) + ", tofuEvalOut)\n}\n")
	mainRel, _ := filepath.Rel(root, filepath.Join(module, goMainDir, "main.go"))
	return program{argv: []string{"go", "run", "./" + goMainDir}, dir: module, files: map[string]string{filepath.ToSlash(mainRel): src.String()}, source: source}, nil
}

func rustProgram(root, from string, req request, marker string) (program, error) {
	crate, found := upTo(root, dirOf(from), "Cargo.toml")
	if !found {
		return program{}, fmt.Errorf("no Cargo.toml is at %s or above it inside the project", dirOf(from))
	}
	var src strings.Builder
	if contract.IsFile(filepath.Join(crate, "src", "lib.rs")) {
		library, err := libraryNameOf(filepath.Join(crate, "Cargo.toml"))
		if err != nil {
			return program{}, err
		}
		src.WriteString("#[allow(unused_imports)]\nuse " + library + "::*;\n\n")
	}
	src.WriteString(rustQuoted + "\nfn main() {\n")
	source := generated{file: regexp.MustCompile(generatedLead + rustBinName + `\.rs:(\d+):(\d+)`)}
	source.write(&src, req, "    let tofu_eval_started = std::time::Instant::now();\n", "    let tofu_eval_value = ")
	src.WriteString(";\n" +
		"    let tofu_eval_ms = tofu_eval_started.elapsed().as_secs_f64() * 1000.0;\n" +
		"    println!(\"\\n{}\\n{{\\\"value\\\":{},\\\"type\\\":{},\\\"ms\\\":{}}}\", " + strconv.Quote(marker) +
		", tofu_eval_quoted(&format!(\"{:?}\", tofu_eval_value)), tofu_eval_quoted(std::any::type_name_of_val(&tofu_eval_value)), tofu_eval_ms);\n}\n")
	binRel, _ := filepath.Rel(root, filepath.Join(crate, "src", "bin", rustBinName+".rs"))
	return program{argv: []string{"cargo", "run", "--quiet", "--color", "never", "--bin", rustBinName}, dir: crate, inTree: true, files: map[string]string{filepath.ToSlash(binRel): src.String()}, source: source}, nil
}

func tsProgram(root, from string, req request) (program, error) {
	module, err := moduleFile(from, req.Path)
	if err != nil {
		return program{}, err
	}
	dir, _ := upTo(root, dirOf(from), "package.json")
	prog := program{argv: []string{"node", "--experimental-strip-types", "--no-warnings"}, dir: cmp.Or(dir, root), script: nodeRunner, runner: "eval.mjs",
		env: []string{"TOFU_EVAL_MODULE=" + module, "TOFU_EVAL_EXPR=" + req.Expr, "TOFU_EVAL_SETUP=" + req.Setup}}
	tsx := filepath.Join("node_modules", ".bin", "tsx")
	if runtime.GOOS == "windows" {
		tsx += ".cmd"
	}
	if at, found := upTo(root, dirOf(from), tsx); found {
		prog.argv = []string{filepath.Join(at, tsx)}
	}
	return prog, nil
}

func pythonProgram(root, from string, req request) (program, error) {
	module, err := moduleFile(from, req.Path)
	if err != nil {
		return program{}, err
	}
	dotted := ""
	if module != "" {
		rel, _ := filepath.Rel(root, strings.TrimSuffix(module, ".py"))
		dotted = strings.TrimSuffix(strings.ReplaceAll(filepath.ToSlash(rel), "/", "."), ".__init__")
	}
	return program{argv: []string{pythonIn(root), "-B"}, dir: root, script: pythonRunner, runner: "eval.py", env: []string{
		"TOFU_EVAL_ROOT=" + root, "TOFU_EVAL_DIR=" + dirOf(from), "TOFU_EVAL_MODULE=" + dotted, "TOFU_EVAL_EXPR=" + req.Expr, "TOFU_EVAL_SETUP=" + req.Setup,
	}}, nil
}

func moduleFile(from, requested string) (string, error) {
	switch info, err := os.Stat(from); {
	case requested == "":
		return "", nil
	case err != nil:
		return "", fmt.Errorf("path %s cannot be read: %w", requested, err)
	case info.IsDir():
		return "", fmt.Errorf("path %s is a directory: name the file whose exports the expression uses", requested)
	}
	return from, nil
}

func pythonIn(root string) string {
	for _, venv := range []string{filepath.Join(root, ".venv", "Scripts", "python.exe"), filepath.Join(root, ".venv", "bin", "python"), filepath.Join(root, "venv", "Scripts", "python.exe"), filepath.Join(root, "venv", "bin", "python")} {
		if contract.IsFile(venv) {
			return venv
		}
	}
	for _, name := range []string{"python", "python3"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return "python3"
}

func standardPackagesNamed(ctx context.Context, code string, imports map[string]string) []string {
	named := map[string]bool{}
	for path, name := range imports {
		named[cmp.Or(name, filepath.Base(path))] = true
	}
	var wanted []string
	for _, match := range regexp.MustCompile(`\b([a-z][a-z0-9]*)\.[A-Z]`).FindAllStringSubmatch(code, -1) {
		if !named[match[1]] && !slices.Contains(wanted, match[1]) {
			wanted = append(wanted, match[1])
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	goroot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(wanted, func(name string) bool {
		info, err := os.Stat(filepath.Join(strings.TrimSpace(string(goroot)), "src", name))
		return err != nil || !info.IsDir()
	})
}

func packageNameOf(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if name := entry.Name(); strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			if file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.PackageClauseOnly); err == nil {
				return file.Name.Name
			}
		}
	}
	return ""
}

func modulePathOf(goMod string) (string, error) {
	body, err := os.ReadFile(goMod)
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(body)) {
		if rest, found := strings.CutPrefix(strings.TrimSpace(line), "module "); found {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", errors.New(goMod + " names no module")
}

func libraryNameOf(cargoToml string) (string, error) {
	body, err := os.ReadFile(cargoToml)
	if err != nil {
		return "", err
	}
	names, section := map[string]string{}, ""
	for line := range strings.Lines(string(body)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if key, value, found := strings.Cut(line, "="); found && strings.TrimSpace(key) == "name" {
			names[section] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	name := cmp.Or(names["[lib]"], names["[package]"])
	if name == "" {
		return "", errors.New(cargoToml + " names no package")
	}
	return strings.ReplaceAll(name, "-", "_"), nil
}

func upTo(root, from string, name ...string) (string, bool) {
	for at := from; ; at = filepath.Dir(at) {
		if contract.IsFile(filepath.Join(append([]string{at}, name...)...)) {
			return at, true
		}
		if at == root || filepath.Dir(at) == at {
			return "", false
		}
	}
}

func dirOf(path string) string {
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return filepath.Dir(path)
	}
	return path
}

func (g *generated) write(src *strings.Builder, req request, started, lead string) {
	g.setupLine = strings.Count(src.String(), "\n") + 1
	if req.Setup != "" {
		g.setupLines = strings.Count(req.Setup, "\n") + 1
		src.WriteString(req.Setup + "\n")
	}
	src.WriteString(started)
	g.exprLine, g.exprLines, g.exprColumn = strings.Count(src.String(), "\n")+1, strings.Count(req.Expr, "\n")+1, len(lead)+1
	src.WriteString(lead + req.Expr)
}

const rustQuoted = `fn tofu_eval_quoted(text: &str) -> String {
    let mut out = String::from("\"");
    for c in text.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            c if (c as u32) < 0x20 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}
`

const nodeRunner = `import vm from "node:vm";
import util from "node:util";
import nodeModule from "node:module";
import { pathToFileURL } from "node:url";

const env = process.env;
const strip = (code) => {
  try {
    return nodeModule.stripTypeScriptTypes(code);
  } catch {
    return code;
  }
};
let out;
try {
  const exported = env.TOFU_EVAL_MODULE ? await import(pathToFileURL(env.TOFU_EVAL_MODULE).href) : {};
  const context = vm.createContext({ console, process, Buffer, URL, URLSearchParams, setTimeout, clearTimeout, setInterval, clearInterval, structuredClone, fetch, TextEncoder, TextDecoder, ...exported });
  if (env.TOFU_EVAL_SETUP) new vm.Script(strip(env.TOFU_EVAL_SETUP), { filename: "setup" }).runInContext(context);
  const started = performance.now();
  const value = await new vm.Script(strip(env.TOFU_EVAL_EXPR), { filename: "expression" }).runInContext(context);
  const ms = performance.now() - started;
  const type = value === null ? "null" : typeof value === "object" ? (value.constructor?.name ?? "object") : typeof value;
  out = { value: util.inspect(value, { depth: 4 }), type, ms };
} catch (error) {
  const said = error?.stack ? String(error.stack) : String(error);
  out = { error: said.split("\n").filter((line) => !line.includes("node:") && !line.includes(import.meta.url)).join("\n") };
}
process.stdout.write("\n" + env.TOFU_EVAL_MARKER + "\n" + JSON.stringify(out) + "\n");
`

const pythonRunner = `import importlib
import json
import os
import sys
import time
import traceback

env = os.environ
sys.path[0:1] = [env["TOFU_EVAL_ROOT"], env["TOFU_EVAL_DIR"]]
names = {"__builtins__": __builtins__}
try:
    if env.get("TOFU_EVAL_MODULE", ""):
        names.update(vars(importlib.import_module(env["TOFU_EVAL_MODULE"])))
    names["__name__"] = "__tofu_eval__"
    if env.get("TOFU_EVAL_SETUP", ""):
        exec(compile(env["TOFU_EVAL_SETUP"], "setup", "exec"), names)
    started = time.perf_counter()
    value = eval(compile(env["TOFU_EVAL_EXPR"], "expression", "eval"), names)
    ms = (time.perf_counter() - started) * 1000
    kind = type(value)
    named = kind.__qualname__ if kind.__module__ == "builtins" else kind.__module__ + "." + kind.__qualname__
    out = {"value": repr(value), "type": named, "ms": ms}
except BaseException as error:
    out = {"error": "".join(traceback.format_exception(type(error), error, error.__traceback__.tb_next)).rstrip()}
sys.stdout.write("\n" + env["TOFU_EVAL_MARKER"] + "\n" + json.dumps(out) + "\n")
`
