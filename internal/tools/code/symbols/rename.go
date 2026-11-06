package symbols

import (
	"context"
	"errors"
	"fmt"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/lsp"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit/patch"
	"tofu/internal/tools/files/glob"
)

type renamed struct {
	by        string
	before    map[string]string
	after     map[string]string
	sites     int
	clash     string
	errors    [2]map[string][]lsp.Problem
	unchecked []string
}

func (s Symbols) changing(ctx context.Context, args symbolsArgs, under []string) (contract.Result, error) {
	target, err := s.target(args.Name, under)
	if err != nil {
		return contract.Result{}, err
	}
	project, err := glob.FilesUnder(s.root, ".", args.IncludeIgnored)
	if err != nil {
		return contract.Result{}, fmt.Errorf("symbols: %w", err)
	}
	served := filepath.Ext(target.at.Path) != ".go"
	if served && s.servers == nil {
		return contract.Result{}, errors.New("symbols: no language server is wired into this run, so " + args.Name + " cannot be resolved beyond its name")
	}
	if args.Mode == modeImpact {
		if served {
			return s.impactServed(ctx, target, project.Files)
		}
		return s.impactGo(ctx, target)
	}
	if !regexp.MustCompile(`^[\p{L}_$][\p{L}\p{N}_$]*$`).MatchString(args.To) || !served && !token.IsIdentifier(args.To) {
		return contract.Result{}, fmt.Errorf("symbols: to %q is not an identifier, and a rename needs the new name as one", args.To)
	}
	if args.To == target.symbol[strings.LastIndex(target.symbol, ".")+1:] {
		return contract.Result{}, fmt.Errorf("symbols: %s is already called %s", target.symbol, args.To)
	}
	var plan renamed
	if served {
		plan, err = s.renameServed(ctx, target, args.To, project.Files)
	} else {
		plan, err = s.renameGo(ctx, target, args.To)
	}
	if err != nil {
		return contract.Result{}, fmt.Errorf("symbols: %w. nothing was written", err)
	}
	return s.finish(target, args, plan)
}

func (s Symbols) target(name string, files []string) (declared, error) {
	last := name[strings.LastIndex(name, ".")+1:]
	found := slices.DeleteFunc(s.declarations(last, files), func(one declared) bool {
		return one.symbol != name && (strings.Contains(name, ".") || !strings.HasSuffix(one.symbol, "."+name))
	})
	switch len(found) {
	case 0:
		return declared{}, fmt.Errorf("symbols: nothing under this path declares %s, so there is nothing to rename or weigh", name)
	case 1:
		return found[0], nil
	}
	named := make([]string, len(found))
	for i, one := range found {
		named[i] = fmt.Sprintf("%s at %s:%d", one.label(), s.shown(one.at.Path), one.at.Line)
	}
	return declared{}, fmt.Errorf("symbols: %s names %d declarations, %s, and a rename or an impact takes exactly one: name a method Type.method, or narrow path to the file", name, len(found), strings.Join(named, " and "))
}

func (s Symbols) renameServed(ctx context.Context, target declared, to string, files []string) (renamed, error) {
	server, _ := lsp.ServerFor(target.at.Path)
	plan := renamed{by: server.Program, before: map[string]string{}, after: map[string]string{}}
	root := server.RootOf(string(s.root), target.at.Path)
	err := s.servers.Use(server, root, func(client *lsp.Client) error {
		if err := client.Open(s.under(server, root, files)...); err != nil {
			return err
		}
		asking, cancel := context.WithTimeout(ctx, konst.LspRequestMillis*time.Millisecond)
		defer cancel()
		neighbours, err := client.Neighbours(asking, target.at)
		if err != nil {
			return err
		}
		for _, neighbour := range neighbours {
			if neighbour.Name == to {
				plan.clash = fmt.Sprintf("%s:%d already declares %s in the same scope as %s", s.shown(target.at.Path), neighbour.Line, to, target.symbol)
				return nil
			}
		}
		edits, err := client.Rename(asking, target.at, to)
		if err != nil {
			return err
		}
		paths := slices.Sorted(maps.Keys(edits))
		for _, path := range paths {
			if filepath.IsAbs(s.shown(path)) {
				return fmt.Errorf("%s would also change %s, outside this project", server.Program, path)
			}
		}
		if err := client.Open(paths...); err != nil {
			return err
		}
		check := func() (map[string][]lsp.Problem, []string) {
			waiting, stop := context.WithTimeout(ctx, konst.SymbolsDiagnosticsWaitMillis*time.Millisecond)
			defer stop()
			return client.Errors(waiting, paths)
		}
		plan.errors[0], _ = check()
		for _, path := range paths {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			plan.before[path], plan.after[path] = string(body), lsp.Apply(string(body), edits[path])
			plan.sites += len(edits[path])
			if err := client.Show(path, plan.after[path]); err != nil {
				return err
			}
		}
		plan.errors[1], plan.unchecked = check()
		return client.Open(paths...)
	})
	return plan, err
}

func (s Symbols) added(plan renamed) []string {
	var lines []string
	for _, path := range slices.Sorted(maps.Keys(plan.errors[1])) {
		had := map[string]int{}
		for _, problem := range plan.errors[0][path] {
			had[problem.Message]++
		}
		for _, problem := range plan.errors[1][path] {
			if had[problem.Message]--; had[problem.Message] < 0 {
				lines = append(lines, fmt.Sprintf("  %s:%d %s", s.shown(path), problem.Line, problem.Message))
			}
		}
	}
	return lines
}

func (s Symbols) finish(target declared, args symbolsArgs, plan renamed) (contract.Result, error) {
	head := fmt.Sprintf("rename %s to %s, resolved by %s", target.symbol, args.To, plan.by)
	if plan.clash != "" {
		return contract.Result{}, fmt.Errorf("symbols: %s is refused: %s. nothing was written", head, plan.clash)
	}
	fresh := s.added(plan)
	if args.Apply && len(fresh) > 0 {
		return contract.Result{}, fmt.Errorf("symbols: %s is refused: it would add %d errors. nothing was written\n%s", head, len(fresh), strings.Join(fresh, "\n"))
	}
	var out strings.Builder
	command, paths := "rename "+target.symbol+" "+args.To, slices.Sorted(maps.Keys(plan.after))
	fmt.Fprintf(&out, "%s: %d sites in %d files\n", head, plan.sites, len(paths))
	for _, path := range paths {
		out.WriteString(patch.Unified(s.shown(path), plan.before[path], plan.after[path]))
	}
	switch {
	case len(fresh) > 0:
		fmt.Fprintf(&out, "new errors: %d\n%s\n", len(fresh), strings.Join(fresh, "\n"))
	case len(plan.unchecked) > 0:
		unchecked := make([]string, len(plan.unchecked))
		for i, path := range plan.unchecked {
			unchecked[i] = s.shown(path)
		}
		fmt.Fprintf(&out, "new errors: unknown for %s, which %s did not check within %d s\n", strings.Join(unchecked, ", "), plan.by, konst.SymbolsDiagnosticsWaitMillis/1000)
	default:
		fmt.Fprintf(&out, "new errors: none\n")
	}
	if !args.Apply {
		out.WriteString("preview: nothing was written. call again with apply true to write it\n")
		return contract.Result{Content: out.String(), Command: command}, nil
	}
	for _, path := range paths {
		if body, err := os.ReadFile(path); err != nil || string(body) != plan.before[path] {
			return contract.Result{}, fmt.Errorf("symbols: %s changed on disk while the rename was planned. nothing was written", s.shown(path))
		}
	}
	for written, path := range paths {
		if err := os.WriteFile(path, []byte(plan.after[path]), 0o644); err != nil {
			return contract.Result{}, fmt.Errorf("symbols: writing %s failed after %d of %d files were written: %w", s.shown(path), written, len(paths), err)
		}
	}
	fmt.Fprintf(&out, "written: %d files\n", len(paths))
	return contract.Result{Content: out.String(), Command: command}, nil
}
