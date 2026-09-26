package main

import "testing"

const (
	modelsFromTheBinaryAlone = `claude-sub/claude-opus-5, codex-sub/gpt-5.6-sol 7 of 18 usable

  claude-sub  claude-sub/claude-opus-5 (kind llm, pays subscription) by
                default on --wire anthropic, a 1000000 token window, spends
                5h and 7d
              also allowed, claude-sub/claude-haiku-4-5-20251001 (kind llm,
                pays subscription), claude-sub/claude-sonnet-5 (kind llm,
                pays subscription)
              2 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              6 excluded, a previous generation the account still serves,
                superseded by claude-opus-5 and claude-sonnet-5

  codex-sub   codex-sub/gpt-5.6-sol (kind llm, pays subscription) by default
                on --wire codex, a 1050000 token window, spends 5h and 7d
              also allowed, codex-sub/gpt-5.6-luna (kind llm, pays
                subscription), codex-sub/gpt-5.6-terra (kind llm, pays
                subscription)
              1 excluded, a previous generation the account still serves,
                superseded by gpt-5.6-sol
              1 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              1 excluded, nobody has ruled on it, so tofu does not send it
                until somebody does

  key         typesafe/jev-latest (kind classifier, pays key), use allowed

  windows     16 of 18 models take a context window from the snapshot of
                models.dev taken on 2026-09-21, and tofu models --refresh
                reads the table again

  roles       orchestrator: the model that plans and hands work to
                sub-agents. nothing is bound, so it runs on the subscription
                default

              (unnamed sub-agent): a spawn that names no sub-agent. nothing
                is bound, so it runs on the orchestrator's model
`

	modelsWithAProjectLayerAndAHomeRegistry = `claude-sub/claude-opus-5, codex-sub/gpt-5.6-sol 8 of 18 usable

  claude-sub  claude-sub/claude-opus-5 (kind llm, pays subscription) by
                default on --wire anthropic, a 123456 token window, spends
                5h and 7d
              also allowed, claude-sub/claude-haiku-4-5-20251001 (kind llm,
                pays subscription), claude-sub/claude-opus-4-5-20251101
                (kind llm, pays subscription), claude-sub/claude-sonnet-5
                (kind llm, pays subscription) [orchestrator]
              2 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              5 excluded, a previous generation the account still serves,
                superseded by claude-opus-5 and claude-sonnet-5

  codex-sub   codex-sub/gpt-5.6-sol (kind llm, pays subscription) by default
                on --wire codex, a 0 token window, spends 5h and 7d
              also allowed, codex-sub/gpt-5.6-luna (kind llm, pays
                subscription), codex-sub/gpt-5.6-terra (kind llm, pays
                subscription)
              1 excluded, a previous generation the account still serves,
                superseded by gpt-5.6-sol
              1 excluded, the owner on 2026-09-19, not fable or astra for
                now, those are not cheap. A current preference recorded as
                data, not a permanent rule
              1 excluded, nobody has ruled on it, so tofu does not send it
                until somebody does

  key         typesafe/jev-latest (kind classifier, pays key), use allowed

  windows     1 of 18 models take a context window from the table this test
                wrote, and tofu models --refresh reads the table again

  roles       (unnamed sub-agent): a spawn that names no sub-agent. nothing
                is bound, so it runs on the orchestrator's model
`

	homeRegistryOfOneWindow = `{"from":"the table this test wrote","windows":{"anthropic/claude-opus-5":123456}}`
)

func TestE2EModelsMergesTheProjectLayerOverTheBinaryAndReadsTheRegistryFromHome(t *testing.T) {
	shipped := newProject(t, "shipped")
	sameText(t, "a project that overrides nothing",
		shipped.run(t, exitOK, "models"), modelsFromTheBinaryAlone)

	layered := newProject(t, "layered")
	writeFile(t, layered.dir, ".tofu/models/anthropic/claude-opus-4-5-20251101.yaml", "use: allowed\n")
	writeFile(t, layered.dir, ".tofu/roles/turn.yaml", "model: claude-sub/claude-sonnet-5\n")
	writeFile(t, layered.home, ".tofu/model-windows.json", homeRegistryOfOneWindow)
	sameText(t, "a project that allows one excluded model, binds the orchestrator through the legacy turn.yaml, and carries its own window table",
		layered.run(t, exitOK, "models"), modelsWithAProjectLayerAndAHomeRegistry)
}
