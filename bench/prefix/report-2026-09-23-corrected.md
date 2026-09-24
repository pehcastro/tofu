# bench prefix, the per-session cache rewrite with the real composed prompt: 2026-09-23

Machine: DESKTOP-AHUN9RO. No live model call, no network call: every figure comes from `turn.Compose` and `Request.Encode`, called directly.

`go test ./bench/prefix/... -count=1` passes, 5 test functions.

## Headline

**With the real composed system prompt for a real task, 9 rules firing on top of the two builtin blocks, the per-session rewrite is 4492 bytes, an estimated 1123 tokens.** That is TOFU-535's own figure of 4135 bytes plus 357 bytes: TOFU-538 moved the working-directory line and the spawn addendum out of `cmd/tofu/run.go` into exported names in `internal/turn`, so `bench/prefix.RealToolGuidance` composes the real text a `tofu run` sends rather than a subset of it. 357 bytes of that correction is the working-directory line and the spawn addendum this package could not reach before; the total gap against TOFU-532's floor of 2209 bytes and 552 tokens is now 2283 bytes, and 2283 of it is the 9 rules that fired for this task.

## The task and the rules it fired

Task: "write a fix for the flaky test in internal/turn/compose_test.go". 9 rules fired: no_worktree, ownership, em_dash, comments, flake_disagreement, skipped_test_budget, test_assertion, test_boundary_cases, test_mock_boundary.

## Both arms

| Arm | System array bytes | Estimated tokens | Rewritten per session |
|---|---|---|---|
| as sent today, billing block present | 4492 | 1123 | yes, the whole array |
| billing block absent, the arm | 4265 | 1066 | no, 0 bytes |
| builtin blocks only, no rules fired | 2209 | 552 | yes, the whole array |

The billing block absent arm is the same request with `Request.Encode(false)` rather than `Encode(true)`: no per-session attestation block is injected, so the two fingerprinted first messages produce byte-identical system arrays and nothing is rewritten between sessions. The array itself is still 4265 bytes; the difference from the billing-present array (4492 bytes) is the billing header and the identity line the wire injects only on the OAuth path.

## Reproduces from its source

`TestGeneratorReproducesTheSameBytesTwice` composes the same rules twice and measures both: run one 4492 bytes, 1123 tokens, run two 4492 bytes, 1123 tokens, identical.

## The two copied strings in prefix_bound_test.go

`internal/llm/wire/anthropic/prefix_bound_test.go:17` hardcodes `realisticSystemPromptFloor`, a literal copy of `internal/turn/prompt.go:14`'s `PreferTheToolOverTheShell` followed by `internal/turn/compose.go:15`'s `TheFormatContract`, joined and wrapped by hand in the same `"[%s, from %s]\n%s"` shape `Composed.System()` builds. Both source constants are exported now, so that file could import `tofu/internal/turn` and write `turn.PreferTheToolOverTheShell` and `turn.TheFormatContract` directly, closing the drift TOFU-532 named as a known risk. It should not be deleted: it is `internal/llm/wire/anthropic`'s own bound on what it injects by itself, `injectedSystemPrefixBytesBound`, which this package cannot police because bench measures the wire, it does not gate it. The realistic bound in that file is now the redundant one: this package's `TestRewritePerSessionWithTheBillingBlock` measures the same shape with real rules on top and a rerunnable number, so `realisticSystemPrefixBytesBound` and `TestVaryingSystemPrefixBytesWithTheCallersSystemPrompt` are a lower bound that never moves, kept only as the wire package's own regression fence.

