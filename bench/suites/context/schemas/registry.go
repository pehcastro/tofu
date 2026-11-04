package schemas

import (
	iturn "tofu/internal/agent/loop"
	"tofu/internal/models/llm/core"
	"tofu/internal/tools/contract"
	"tofu/internal/tools/registry"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func FullRegistry(dir string) iturn.Registry {
	readTool := must(iturn.NewReadTool(dir))
	writeTool := must(iturn.NewWriteTool(dir))
	bashTool := must(iturn.NewBashTool(dir))
	shell := must(registry.Checked(dir, []contract.Tool{bashTool}))[0]

	verbTools := must(registry.NewVerbs(dir))
	full := append([]contract.Tool{
		readTool, writeTool, shell, registry.NewPlan(),
		must(registry.NewProject(dir)), must(registry.NewGlob(dir)), must(registry.NewSearch(dir)),
		must(registry.NewSymbols(dir)), must(registry.NewEdit(dir)), must(registry.NewGitHubPRDiff(dir)),
	}, verbTools...)
	full = registry.NewMemo().Wrap(full)

	full = append(full, registry.NewQuote(nil, ""))
	full = append(full, iturn.NewSpawnTool("schemas-probe", iturn.Config{}, nil))
	full = append(full, must(iturn.NewArtifacts(dir, true)).FetchTool())

	return iturn.NewRegistry(full...)
}

const FullToolCount = 19

func Diff(a, b []string) (onlyA, onlyB []string) {
	inB := map[string]bool{}
	for _, name := range b {
		inB[name] = true
	}
	inA := map[string]bool{}
	for _, name := range a {
		inA[name] = true
		if !inB[name] {
			onlyA = append(onlyA, name)
		}
	}
	for _, name := range b {
		if !inA[name] {
			onlyB = append(onlyB, name)
		}
	}
	return onlyA, onlyB
}

func Names(defs []core.Tool) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}
