package schemas

import (
	"tofu/internal/llm"
	iturn "tofu/internal/turn"
	"tofu/internal/turn/tools"
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
	shell := must(tools.Checked(dir, []iturn.Tool{bashTool}))[0]

	verbTools := must(tools.NewVerbs(dir))
	full := append([]iturn.Tool{
		readTool, writeTool, shell, tools.NewPlan(),
		must(tools.NewProject(dir)), must(tools.NewGlob(dir)), must(tools.NewSearch(dir)),
		must(tools.NewSymbols(dir)), must(tools.NewEdit(dir)), must(tools.NewGitHubPRDiff(dir)),
	}, verbTools...)
	full = tools.NewMemo().Wrap(full)

	full = append(full, tools.NewQuote(nil, ""))
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

func Names(defs []llm.Tool) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Name
	}
	return names
}
