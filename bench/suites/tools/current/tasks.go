package main

import "strings"

type step struct {
	tool, args string
	cleanup    bool
}

type arm struct {
	name  string
	build string
	steps []step
}

type task struct {
	name, fixture, ask string
	arms               []arm
	answers            func(at int, results []result) bool
}

type result struct {
	content, outcome string
}

const (
	oldBuild  = "0.5.21"
	oldCommit = "ad1eea2a"
	devBuild  = "change"
	fixPrefix = "fix a failing test, "
)

func fixTest(fixture, source, command, old, fixed string) task {
	read := step{"read", `{"path":"` + source + `"}`, false}
	edit := step{"edit", `{"path":"` + source + `","old_string":"` + old + `","new_string":"` + fixed + `"}`, false}
	bash := step{"bash", `{"command":"` + command + `"}`, false}
	tool := step{"test", `{"path":"."}`, false}
	return task{
		name:    fixPrefix + fixture,
		fixture: fixture,
		ask:     "a test fails, fix it",
		arms: []arm{
			{"0.5.21, bash", oldBuild, []step{bash, read, edit, bash}},
			{"change, bash", devBuild, []step{bash, read, edit, bash}},
			{"change, test", devBuild, []step{tool, read, edit, tool}},
		},
		answers: func(at int, results []result) bool {
			return at == 3 && results[0].outcome == "failed" && results[3].outcome != "failed"
		},
	}
}

func waitServer() task {
	start := step{"bash", `{"command":"go run .","background":true}`, false}
	stop := step{"shell", `{"op":"stop","name":"bash-1"}`, true}
	return task{
		name:    "wait for a server",
		fixture: "server",
		ask:     "start the server and wait until it listens",
		arms: []arm{
			{"0.5.21, wait until", oldBuild, []step{start, {"shell", `{"op":"wait","name":"bash-1","until":"listening on"}`, false}, stop}},
			{"change, wait for", devBuild, []step{start, {"shell", `{"op":"wait","name":"bash-1","for":"listening on"}`, false}, stop}},
		},
		answers: func(at int, results []result) bool {
			return strings.Contains(results[at].content, "listening on")
		},
	}
}

func readTwice(between string, steps []step) task {
	return task{
		name:    "read a file twice, " + between,
		fixture: "go",
		ask:     "read order.go",
		arms: []arm{
			{"0.5.21", oldBuild, steps},
			{"change", devBuild, steps},
		},
		answers: func(at int, results []result) bool {
			return at == len(steps)-1 && strings.Contains(results[0].content, "func (o Order) Discounted")
		},
	}
}

func tasks() []task {
	read := step{"read", `{"path":"order.go"}`, false}
	return []task{
		fixTest("go", "order.go", "go test -v .", "return o.Total() - 100", "return o.Total() * 9 / 10"),
		fixTest("rust", "src/lib.rs", "cargo test", "self.total() - 100", "self.total() * 9 / 10"),
		fixTest("python", "order.py", "python -m unittest -v", "return self.total() - 100", "return round(self.total() * 9 / 10)"),
		waitServer(),
		readTwice("back to back", []step{read, read}),
		readTwice("go vet between", []step{read, {"bash", `{"command":"go vet ."}`, false}, read}),
	}
}
