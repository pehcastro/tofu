package loop

import (
	"context"

	"tofu/internal/tools/contract"
)

type (
	ReadLedger    = contract.ReadLedger
	LineChange    = contract.LineChange
	LineEndings   = contract.LineEndings
	subAgentKey   = contract.SubAgentKey
	shellOwnerKey = contract.ShellOwnerKey
)

func NewReadLedger() *ReadLedger { return contract.NewReadLedger() }

func ChangeOf(before, after string) LineChange { return contract.ChangeOf(before, after) }

func EndingsOf(body string) (string, LineEndings) { return contract.EndingsOf(body) }

func AsLF(text string) string { return contract.AsLF(text) }

func RepairPath(requested, noun string, found []string) (string, string, error) {
	return contract.RepairPath(requested, noun, found)
}

func SubAgentAsking(ctx context.Context) string { return contract.SubAgentAsking(ctx) }

func shellOwnerFrom(ctx context.Context) string { return contract.ShellOwnerFrom(ctx) }

func ledgerKey(path string) string { return contract.LedgerKey(path) }

func lookalikes(r contract.Root, requested string) ([]string, error) {
	return contract.Lookalikes(r, requested)
}

func realPaths(r contract.Root, candidates []string) []string {
	return contract.RealPaths(r, candidates)
}

func gitListing(r contract.Root) ([]string, error) { return contract.GitListing(r) }

func walkListing(r contract.Root) ([]string, error) { return contract.WalkListing(r) }

func findUp(dir string, name ...string) (string, bool) { return contract.FindUp(dir, name...) }

func isFile(path string) bool { return contract.IsFile(path) }

func absolute(dir string) string { return contract.Absolute(dir) }

func listed(ids []string) string { return contract.Listed(ids) }

func failure(content string) contract.Result { return contract.Failure(content) }
