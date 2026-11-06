package edit

import (
	"context"
	"time"

	"tofu/internal/konst"
	"tofu/internal/tools/code/typecheck/checkers"
	"tofu/internal/tools/contract"
)

func Checked(ctx context.Context, typecheckers *checkers.Typecheckers, root contract.Root, resolved, result string) string {
	checks, skipped := checkers.EditChecks(string(root), resolved)
	switch {
	case skipped != "":
		return result + "\n\ntypecheck skipped: " + skipped
	case len(checks) == 0:
		return typecheckers.Typechecked(ctx, resolved, result)
	}
	text, _ := checkers.RunChecks(ctx, checks, konst.TypecheckInlineFirstMillis*time.Millisecond)
	return result + "\n\n" + text
}
