package turn

import (
	"context"
	"fmt"
)

type Account struct {
	ID       int64
	Model    Model
	Headroom float64
	Window   string
}

type Accounts struct {
	Pick func(context.Context) (Account, error)
	Next func(context.Context, Account) (Account, bool, error)
}

func movedAccountWords(from, to Account, freshTokens int) string {
	return fmt.Sprintf(
		"account #%d has no window left, so this session moved to account #%d, which has %.0f%% of its %s window left: "+
			"the move costs one fresh prefix of %d tokens, paid once, because the new session starts on a cold cache",
		from.ID, to.ID, to.Headroom*100, to.Window, freshTokens)
}
