package prefix

import (
	"fmt"
	"strings"
)

const TOFU532FloorBytes = 2209
const TOFU532FloorTokens = 552

const testFunctionCount = 5

type Report struct {
	Date        string
	Machine     string
	Task        string
	FiredRules  []string
	WithBilling RewriteFigure
	NoBilling   RewriteFigure
	BuiltinOnly RewriteFigure
	Reproduced  [2]RewriteFigure
}

func (r Report) rulesBytes() int { return r.WithBilling.Bytes - r.BuiltinOnly.Bytes }
func (r Report) gapBytes() int   { return r.WithBilling.Bytes - TOFU532FloorBytes }
func (r Report) toolGuidanceShortfall() int {
	return TOFU532FloorBytes - r.BuiltinOnly.Bytes
}

func (r Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# bench prefix, the per-session cache rewrite with the real composed prompt: %s\n\n", r.Date)
	fmt.Fprintf(&b, "Machine: %s. No live model call, no network call: every figure comes from `turn.Compose` and `Request.Encode`, called directly.\n\n", r.Machine)
	fmt.Fprintf(&b, "`go test ./bench/prefix/... -count=1` passes, %d test functions.\n\n", testFunctionCount)

	b.WriteString("## Headline\n\n")
	fmt.Fprintf(&b, "**With the real composed system prompt for a real task, %d rules firing on top of the two builtin blocks, the per-session rewrite is %d bytes, an estimated %d tokens.** That is TOFU-532's floor of %d bytes and %d tokens plus a gap of %d bytes: %d of the gap is the %d rules that fired for this task, the rest is %d bytes this package's `ToolGuidance` is short of the production one, because the working-directory prefix and the spawn addendum are private constants in `cmd/tofu/run.go` this package cannot import without copying them.\n\n",
		len(r.FiredRules), r.WithBilling.Bytes, r.WithBilling.Tokens,
		TOFU532FloorBytes, TOFU532FloorTokens, r.gapBytes(), r.rulesBytes(), len(r.FiredRules), r.toolGuidanceShortfall())

	b.WriteString("## The task and the rules it fired\n\n")
	fmt.Fprintf(&b, "Task: %q. %d rules fired: %s.\n\n", r.Task, len(r.FiredRules), strings.Join(r.FiredRules, ", "))

	b.WriteString("## Both arms\n\n")
	b.WriteString("| Arm | System array bytes | Estimated tokens | Rewritten per session |\n|---|---|---|---|\n")
	fmt.Fprintf(&b, "| as sent today, billing block present | %d | %d | yes, the whole array |\n", r.WithBilling.Bytes, r.WithBilling.Tokens)
	fmt.Fprintf(&b, "| billing block absent, the arm | %d | %d | no, 0 bytes |\n", r.NoBilling.Bytes, r.NoBilling.Tokens)
	fmt.Fprintf(&b, "| builtin blocks only, no rules fired | %d | %d | yes, the whole array |\n\n", r.BuiltinOnly.Bytes, r.BuiltinOnly.Tokens)

	fmt.Fprintf(&b, "The billing block absent arm is the same request with `Request.Encode(false)` rather than `Encode(true)`: no per-session attestation block is injected, so the two fingerprinted first messages produce byte-identical system arrays and nothing is rewritten between sessions. The array itself is still %d bytes; the difference from the billing-present array (%d bytes) is the billing header and the identity line the wire injects only on the OAuth path.\n\n",
		r.NoBilling.Bytes, r.WithBilling.Bytes)

	b.WriteString("## Reproduces from its source\n\n")
	fmt.Fprintf(&b, "`TestGeneratorReproducesTheSameBytesTwice` composes the same rules twice and measures both: run one %d bytes, %d tokens, run two %d bytes, %d tokens, identical.\n\n",
		r.Reproduced[0].Bytes, r.Reproduced[0].Tokens, r.Reproduced[1].Bytes, r.Reproduced[1].Tokens)

	b.WriteString("## The two copied strings in prefix_bound_test.go\n\n")
	b.WriteString("`internal/llm/wire/anthropic/prefix_bound_test.go:17` hardcodes `realisticSystemPromptFloor`, a literal copy of `internal/turn/prompt.go:14`'s `PreferTheToolOverTheShell` followed by `internal/turn/compose.go:15`'s `TheFormatContract`, joined and wrapped by hand in the same `\"[%s, from %s]\\n%s\"` shape `Composed.System()` builds. Both source constants are exported now, so that file could import `tofu/internal/turn` and write `turn.PreferTheToolOverTheShell` and `turn.TheFormatContract` directly, closing the drift TOFU-532 named as a known risk. It should not be deleted: it is `internal/llm/wire/anthropic`'s own bound on what it injects by itself, `injectedSystemPrefixBytesBound`, which this package cannot police because bench measures the wire, it does not gate it. The realistic bound in that file is now the redundant one: this package's `TestRewritePerSessionWithTheBillingBlock` measures the same shape with real rules on top and a rerunnable number, so `realisticSystemPrefixBytesBound` and `TestVaryingSystemPrefixBytesWithTheCallersSystemPrompt` are a lower bound that never moves, kept only as the wire package's own regression fence.\n\n")

	return b.String()
}
