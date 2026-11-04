package registry

import (
	"context"

	"tofu/internal/tools/contract"
	"tofu/internal/tools/human/remember"
	"tofu/internal/tools/shell/jobs"
	"tofu/internal/tools/web"
	"tofu/internal/tools/web/browser"
	"tofu/internal/tools/web/browser/driver"
	"tofu/internal/tools/web/github"
)

type (
	Shells          = jobs.Shells
	BrowserSettings = browser.BrowserSettings
	BrowserTabs     = browser.BrowserTabs
	ScopeJudge      = remember.ScopeJudge
	MemoryOffer     = remember.MemoryOffer
	MemoryPick      = remember.MemoryPick
	MemoryKept      = remember.MemoryKept
	Remember        = remember.Remember
)

func NewGitHubPRDiff(dir string) (github.GitHubPRDiff, error) { return github.NewGitHubPRDiff(dir) }

func NewWeb(config web.Config) []contract.Tool { return web.NewWeb(config) }

func NewBrowserTabs(home string) *BrowserTabs { return browser.NewBrowserTabs(home) }

func NewBrowser(config BrowserSettings) ([]contract.Tool, error) { return browser.NewBrowser(config) }

func BrowserBatch(ctx context.Context, at driver.Endpoint, tabs *BrowserTabs, tab int, steps []byte) (string, int, error) {
	return browser.BrowserBatch(ctx, at, tabs, tab, steps)
}

func WithMemoryPick(ctx context.Context, pick MemoryPick) context.Context {
	return remember.WithMemoryPick(ctx, pick)
}

func WithMemoryKept(ctx context.Context, told func(MemoryKept)) context.Context {
	return remember.WithMemoryKept(ctx, told)
}
