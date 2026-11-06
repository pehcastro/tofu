package symbols

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/lsp"
	"tofu/internal/tools/code/search/engine"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/glob"
)

type Symbols struct {
	root    contract.Root
	servers *lsp.Pool
}

func NewSymbols(dir string) (Symbols, error) {
	root, err := contract.NewRoot(dir)
	return Symbols{root: root}, err
}

func (s Symbols) Name() string { return "symbols" }

func (s Symbols) Definition() core.Tool {
	return core.Tool{
		Name: "symbols",
		Description: "answers where one identifier is declared and every place that calls it, in go, rust, typescript and python files, rather than from a text match: " +
			"go from the go parser, which also lists what a declaration calls, and rust, typescript and python from the project's language server " +
			"(rust-analyzer, typescript-language-server, pyright-langserver), which follows an import alias and also lists the references that are not calls. " +
			"reach for it before changing a function, because it says what the change breaks: search says the name appears in these declarations and this says the name is declared here and called from these three places. " +
			"it reads only .go, .rs, .ts, .tsx and .py files. go matches on the identifier rather than resolving types, and so do rust and typescript when their server is not on PATH, " +
			"so two symbols sharing one name come back as one answer and a call made through an interface, a function value or an alias is not found, and the result says so and names the install command. " +
			"mode impact answers what else must change if this declaration changes: every caller by kind (call, impl, embed, test, ref), the tests that reach it and the packages. " +
			"mode rename renames exactly one declaration and every site that resolves to it, import aliases included, through go/types for go and the language server for the rest: " +
			"it previews a diff per file and the errors the rename would add and writes nothing, and apply true writes it. a rename that would clash with a name already declared there, or add an error, is refused and writes nothing. " +
			"rename and impact need one declaration: name a method Type.method, or narrow path to its file. " +
			glob.IgnoredWalkDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":            map[string]any{"type": "string", "description": "the identifier to look up, exactly as it is spelled in the source, for instance Resolve, or Root.Resolve for a method"},
				"path":            map[string]any{"type": "string"},
				"include_ignored": map[string]any{"type": "boolean"},
				"mode":            map[string]any{"type": "string", "enum": []string{string(modeFind), string(modeImpact), string(modeRename)}, "description": "find when absent"},
				"to":              map[string]any{"type": "string", "description": "the new identifier, for mode rename"},
				"apply":           map[string]any{"type": "boolean", "description": "for mode rename: write the rename rather than preview it"},
			},
			"required": []string{"name"},
		},
	}
}

type mode string

const (
	modeFind   mode = "find"
	modeImpact mode = "impact"
	modeRename mode = "rename"
)

type symbolsArgs struct {
	Name           string `json:"name"`
	Path           string `json:"path"`
	IncludeIgnored bool   `json:"include_ignored"`
	Mode           mode   `json:"mode"`
	To             string `json:"to"`
	Apply          bool   `json:"apply"`
}

func (s Symbols) Run(ctx context.Context, raw json.RawMessage) (contract.Result, error) {
	var args symbolsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return contract.Result{}, fmt.Errorf("symbols: arguments are not the expected shape: %w", err)
	}
	if strings.TrimSpace(args.Name) == "" {
		return contract.Result{}, errors.New("symbols: name is required, and it is one identifier rather than a pattern")
	}
	under := cmp.Or(args.Path, ".")
	listed, err := glob.FilesUnder(s.root, under, args.IncludeIgnored)
	if err != nil {
		return contract.Result{}, fmt.Errorf("symbols: %w", err)
	}
	under = listed.Under
	switch args.Mode {
	case "", modeFind:
	case modeImpact, modeRename:
		return s.changing(ctx, args, listed.Files)
	default:
		return contract.Result{}, fmt.Errorf("symbols: mode %q is none of find, impact and rename", args.Mode)
	}
	files, served := listed.Files, []string(nil)
	if s.servers != nil {
		files, served = s.resolve(ctx, args.Name, listed.Files)
	}
	graph, err := engine.Symbols(string(s.root), files, args.Name)
	if err != nil {
		return contract.Result{}, err
	}
	text := graph.Text
	if len(served) > 0 && graph.Parsed+graph.Unparsed+graph.Unreadable == 0 {
		text = strings.Join(served, "\n")
	} else if len(served) > 0 {
		text += "\n" + strings.Join(served, "\n")
	}
	return contract.Result{
		Content: glob.WithNote("symbols "+args.Name+" under "+under+"\n"+text, listed.Note),
		Command: args.Name + " under " + under,
	}, nil
}
