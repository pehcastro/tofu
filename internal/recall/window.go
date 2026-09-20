package recall

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"tofu/internal/konst"
)

const CeilingVariable = "TOFU_CONTEXT_CEILING"

type Budget struct {
	Model         string
	CeilingTokens int
	Bands         Bands
	WindowTokens  int
	WindowSource  string
	Automatic     bool
	Source        string
}

func BudgetFor(model string, windowTokens int) (Budget, error) {
	budget := Budget{Model: model, WindowTokens: windowTokens, Automatic: true}
	text := strings.TrimSpace(os.Getenv(CeilingVariable))
	if text == "" {
		return budget.At(konst.ContextCeilingTokens, "the ceiling tofu operates under, which is ours and the same on every model"), nil
	}
	set, err := strconv.Atoi(text)
	if err != nil || set <= 0 {
		return Budget{}, fmt.Errorf("recall: %s is %q, and it takes a count of tokens above zero, as in %s=20000", CeilingVariable, text, CeilingVariable)
	}
	return budget.At(set, fmt.Sprintf("%s=%d in the environment of this run", CeilingVariable, set)), nil
}

func (b Budget) At(ceilingTokens int, source string) Budget {
	b.CeilingTokens, b.Bands, b.Source = ceilingTokens, BandsOf(ceilingTokens), source
	return b
}

func (b Budget) Record() string {
	return fmt.Sprintf("on, %s: a %d token ceiling gives a %d token target, %d parts of %d. %s",
		b.Source, b.CeilingTokens, b.Bands.Target(), bandShareOfWindow, konst.BandShareWhole, b.wall())
}

func (b Budget) wall() string {
	if b.WindowTokens <= 0 {
		return "no context window is recorded for " + b.Model +
			", so nothing is refused for being over the wall and the ceiling above still compacts"
	}
	wall := fmt.Sprintf("a request over the %d token window of %s is refused before it is sent", b.WindowTokens, b.Model)
	if b.WindowSource == "" {
		return wall
	}
	return wall + ", " + b.WindowSource
}

func (b Budget) Crossed(cfg Config, c Conversation) bool {
	return Crossed(cfg, b.Bands, c)
}

type OverWindow struct {
	Model         string
	WindowTokens  int
	RequestTokens int
}

func (o *OverWindow) Error() string {
	return fmt.Sprintf("%s holds %d tokens and this request is about %d, so it was not sent",
		o.Model, o.WindowTokens, o.RequestTokens)
}

func (b Budget) RefuseOverWindow(requestTokens int) error {
	if b.WindowTokens <= 0 || requestTokens <= b.WindowTokens {
		return nil
	}
	return &OverWindow{Model: b.Model, WindowTokens: b.WindowTokens, RequestTokens: requestTokens}
}
