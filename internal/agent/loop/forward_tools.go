package loop

import (
	"encoding/json"

	"tofu/internal/models/llm/core"
	"tofu/internal/tools/code/search"
	"tofu/internal/tools/code/test"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/files/edit"
)

type (
	SearchLink      = search.SearchLink
	Typecheckers    = checkers.Typecheckers
	TestRunners     = test.TestRunners
	packageManifest = checkers.PackageManifest
)

const (
	SearchLinkNone      = search.SearchLinkNone
	SearchLinkHit       = search.SearchLinkHit
	SearchLinkUnoffered = search.SearchLinkUnoffered
	tsconfigName        = checkers.TsconfigName
	typecheckUnanswered = checkers.TypecheckUnanswered
	testNoTests         = test.NoTests
)

func EditChanges(raw json.RawMessage) ([]edit.EditChange, error) { return edit.EditChanges(raw) }

func TargetPaths(tool string, raw json.RawMessage) ([]string, error) {
	return edit.TargetPaths(tool, raw)
}

func NoneWritten(changes int) string { return edit.NoneWritten(changes) }

func SearchLinksOf(conversation []core.Message) []SearchLink {
	return search.SearchLinksOf(conversation)
}

func NewTypecheckers() *Typecheckers { return checkers.NewTypecheckers() }

func NewTestRunners() *TestRunners { return test.NewTestRunners() }

func lockfileManager(dir string, manifest packageManifest) string {
	return checkers.LockfileManager(dir, manifest)
}

func vitestPackage(from string) (string, contract.Result) { return test.VitestPackage(from) }
