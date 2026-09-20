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
	Automatic     bool
	Source        string
}

func contextWindows() (map[string]int, error) {
	data, err := catalog.ReadFile("data/context-window-tokens.yaml")
	if err != nil {
		return nil, err
	}
	return numbersByName(data)
}

func windowOf(windows map[string]int, model string) (int, bool) {
	if window, recorded := windows[model]; recorded {
		return window, true
	}
	window, matches := 0, 0
	for name, tokens := range windows {
		if _, bare, qualified := strings.Cut(name, "/"); qualified && bare == model {
			window, matches = tokens, matches+1
		}
	}
	return window, matches == 1
}

func setCeiling() (int, error) {
	text, set := os.LookupEnv(CeilingVariable)
	if !set || strings.TrimSpace(text) == "" {
		return 0, nil
	}
	ceiling, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || ceiling <= 0 {
		return 0, fmt.Errorf("recall: %s is %q, and it takes a count of tokens above zero, as in %s=20000", CeilingVariable, text, CeilingVariable)
	}
	return ceiling, nil
}

func BudgetFor(model string) (Budget, error) {
	ceiling, err := setCeiling()
	if err != nil {
		return Budget{}, err
	}
	if ceiling > 0 {
		return Budget{
			Model:         model,
			CeilingTokens: ceiling,
			Bands:         BandsOf(ceiling),
			Automatic:     true,
			Source:        fmt.Sprintf("%s=%d in the environment of this run", CeilingVariable, ceiling),
		}, nil
	}
	windows, err := contextWindows()
	if err != nil {
		return Budget{}, err
	}
	window, recorded := windowOf(windows, model)
	if !recorded || window <= 0 {
		return Budget{Model: model, Bands: ShippedBands(), Source: "no context window is recorded for " + model}, nil
	}
	return Budget{
		Model:         model,
		CeilingTokens: window,
		Bands:         BandsOf(window),
		Automatic:     true,
		Source:        fmt.Sprintf("the recorded %d token context window of %s", window, model),
	}, nil
}

func (b Budget) Record() string {
	if !b.Automatic {
		return fmt.Sprintf("off, %s: the budget is not read from a window, nothing is compacted or forked on a token count, "+
			"and an unfamiliar model is not truncated on its first step. the bands shown are the shipped ones "+
			"and they measure occupancy without acting on it. set %s to a token count to compact on purpose",
			b.Source, CeilingVariable)
	}
	return fmt.Sprintf("on, %s: a %d token ceiling gives a %d token target, %d parts of %d",
		b.Source, b.CeilingTokens, b.Bands.Target(), bandShareOfWindow, konst.BandShareWhole)
}

func (b Budget) Crossed(cfg Config, c Conversation) bool {
	return b.Automatic && Crossed(cfg, b.Bands, c)
}
