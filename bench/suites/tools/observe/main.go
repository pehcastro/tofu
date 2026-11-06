package main

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type call struct{ tool, args string }

type arm struct {
	name  string
	calls []call
}

type task struct {
	name      string
	answer    []*regexp.Regexp
	tools     arm
	baselines []arm
}

type step struct {
	call
	bytes   int
	spent   time.Duration
	failed  string
	reached bool
}

type outcome struct {
	task, arm, failed string
	tools             bool
	steps             []step
	answeredAt        int
}

type machine struct {
	exe, fixtures, scratch string
	env                    []string
}

const (
	runTimeout  = 3 * time.Minute
	readyWithin = 30 * time.Second
	portToken   = "{port}"
)

func tasks() []task {
	return []task{
		{
			name:   "why GET /orders/1/total says 650, not 675",
			answer: []*regexp.Regexp{regexp.MustCompile(`"total":\s*650`), regexp.MustCompile(`MOCK-SKU-A\W+25\b`), regexp.MustCompile(`for i := 1; i < len\(cents\)`)},
			tools: arm{"tools on: http, db, search", []call{
				{"http", `{"path":"/orders/1/total","port":{port}}`},
				{"db", `{}`},
				{"db", `{"query":"SELECT id, sku, cents FROM items WHERE order_id = 1"}`},
				{"search", `{"pattern":"/orders/{id}/total"}`},
				{"search", `{"pattern":"func orderTotal"}`},
			}},
			baselines: []arm{
				{"bash only, aimed: ls, curl -s, sqlite3, grep -A", []call{
					{"bash", `{"command":"ls"}`},
					{"bash", `{"command":"curl -s localhost:{port}/orders/1/total"}`},
					{"bash", `{"command":"sqlite3 orders.db .schema"}`},
					{"bash", `{"command":"sqlite3 orders.db \"SELECT id, sku, cents FROM items WHERE order_id = 1\""}`},
					{"bash", `{"command":"grep -n \"orders/{id}/total\" main.go"}`},
					{"bash", `{"command":"grep -n -A20 \"func orderTotal\" main.go"}`},
				}},
				{"bash only, whole files: curl -si, ls, cat", []call{
					{"bash", `{"command":"curl -si localhost:{port}/orders/1/total"}`},
					{"bash", `{"command":"ls"}`},
					{"bash", `{"command":"cat seed.sql"}`},
					{"bash", `{"command":"cat main.go"}`},
				}},
			},
		},
		{
			name:   "what one order in GET /orders/export holds",
			answer: []*regexp.Regexp{regexp.MustCompile(`(?i)customer`), regexp.MustCompile(`(?i)status`), regexp.MustCompile(`(?i)sku`), regexp.MustCompile(`(?i)cents`), regexp.MustCompile(`(?i)coupon`)},
			tools:  arm{"tools on: http", []call{{"http", `{"path":"/orders/export","port":{port}}`}}},
			baselines: []arm{
				{"bash only, aimed: curl -s piped to head -c 1000", []call{{"bash", `{"command":"curl -s localhost:{port}/orders/export | head -c 1000"}`}}},
				{"bash only, whole body: curl -s", []call{{"bash", `{"command":"curl -s localhost:{port}/orders/export"}`}}},
			},
		},
	}
}

func main() {
	dev := flag.String("dev", "", "a tofu built from the current tree")
	fixtures := flag.String("fixtures", "bench/suites/tools/fixtures", "the fixture projects, copied fresh for every arm")
	scratch := flag.String("scratch", "", "a directory each arm gets a fresh project copy and a fresh HOME under")
	out := flag.String("out", "", "the report file, otherwise standard output")
	flag.Parse()
	if *dev == "" || *scratch == "" {
		fmt.Fprintln(os.Stderr, "usage: observe -dev <tofu dev exe> -scratch <dir> [-fixtures <dir>] [-out <report.md>]")
		os.Exit(2)
	}
	began := time.Now()
	modules, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "go env GOMODCACHE:", err)
		os.Exit(1)
	}
	m := machine{exe: *dev, fixtures: *fixtures, scratch: filepath.Join(*scratch, began.Format("20060102-150405")),
		env: append(os.Environ(), "GOMODCACHE="+strings.TrimSpace(string(modules)), "GOPROXY=off", "GOMAXPROCS=4")}
	var rows []outcome
	for t, each := range tasks() {
		for a, one := range append([]arm{each.tools}, each.baselines...) {
			rows = append(rows, m.drive(each, one, fmt.Sprintf("t%d-a%d", t+1, a)))
			fmt.Fprintf(os.Stderr, "%s / %s: done\n", each.name, one.name)
		}
	}
	report := render(rows, began, m)
	if *out == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(*out, []byte(report), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (m machine) drive(t task, a arm, id string) outcome {
	o := outcome{task: t.name, arm: a.name, tools: a.name == t.tools.name}
	dir := filepath.Join(m.scratch, id)
	project, home := filepath.Join(dir, "server"), filepath.Join(dir, "home")
	env := append(slices.Clone(m.env), "HOME="+home, "USERPROFILE="+home)
	if err := cmp.Or(os.CopyFS(project, os.DirFS(filepath.Join(m.fixtures, "server"))), os.MkdirAll(home, 0o755)); err != nil {
		o.failed = err.Error()
		return o
	}
	port, stop, err := serve(project, dir, env)
	defer stop()
	if err != nil {
		o.failed = err.Error()
		return o
	}
	seen := make([]bool, len(t.answer))
	for _, c := range a.calls {
		started := time.Now()
		stdout, stderr, err := run(project, env, m.exe, "tool", c.tool, strings.ReplaceAll(c.args, portToken, strconv.Itoa(port)))
		s := step{call: c, bytes: len(stdout), spent: time.Since(started)}
		if err != nil {
			s.failed = firstLine(stdout+stderr, err)
		}
		for i, pattern := range t.answer {
			seen[i] = seen[i] || pattern.MatchString(stdout)
		}
		s.reached = !slices.Contains(seen, false)
		o.steps = append(o.steps, s)
		if s.reached && o.answeredAt == 0 {
			o.answeredAt = len(o.steps)
		}
	}
	return o
}

func serve(project, dir string, env []string) (int, func(), error) {
	stop := func() {}
	binary := filepath.Join(dir, "server.exe")
	if _, output, err := run(project, env, "go", "build", "-o", binary, "."); err != nil {
		return 0, stop, errors.New("go build of the fixture server: " + firstLine(output, err))
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, stop, err
	}
	port := free.Addr().(*net.TCPAddr).Port
	if err := free.Close(); err != nil {
		return 0, stop, err
	}
	server := exec.Command(binary)
	server.Dir, server.Env = project, append(slices.Clone(env), "ORDER_ADDR=127.0.0.1:"+strconv.Itoa(port))
	if err := server.Start(); err != nil {
		return 0, stop, err
	}
	stop = func() {
		_ = server.Process.Kill()
		_ = server.Wait()
	}
	for deadline := time.Now().Add(readyWithin); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			return port, stop, conn.Close()
		}
	}
	return 0, stop, fmt.Errorf("the fixture server did not listen on %d within %s", port, readyWithin)
}

func run(dir string, env []string, name string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func firstLine(output string, err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(output), "\n")
	return cmp.Or(strings.TrimSpace(line), err.Error())
}
