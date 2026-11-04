package registry

import (
	"tofu/internal/tools/code/search"
	"tofu/internal/tools/code/symbols"
	"tofu/internal/tools/code/test"
	"tofu/internal/tools/code/typecheck"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/files/edit"
)

type (
	Typecheck = typecheck.Typecheck
	Test      = test.Test
)

func NewEdit(dir string) (edit.Edit, error) { return edit.NewEdit(dir) }

func NewSearch(dir string) (search.Search, error) { return search.NewSearch(dir) }

func NewSymbols(dir string) (symbols.Symbols, error) { return symbols.NewSymbols(dir) }

func NewTypecheck(dir string, checkers *checkers.Typecheckers) (Typecheck, error) {
	return typecheck.NewTypecheck(dir, checkers)
}

func NewTest(dir string, runners *test.TestRunners) (Test, error) { return test.NewTest(dir, runners) }
