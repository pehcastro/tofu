package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	answerWithin     = 60 * time.Second
	exitWithin       = 30 * time.Second
	notificationMark = "~ "
	timedSnapshot    = "session.listed"
	labelBroadcast   = "session.updated"
)

var (
	earlier        = regexp.MustCompile(`\$\{(\d+)\.([^}]+)\}`)
	racingFields   = []string{"seq", "session"}
	racingByMethod = map[string]string{"turn.steered": "turn"}
	polledMethods  = []string{"memory.changed"}
)

type wireLine struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
}

type exchange struct {
	lines   <-chan string
	talk    strings.Builder
	notes   []string
	answers map[string]any
	unmet   map[string]int
}

func (w work) serve(exe string, args, env []string) (string, error) {
	body, err := os.ReadFile(filepath.Join(w.data, "serve", args[0]+".rpc"))
	if err != nil {
		return "", err
	}
	cmd := w.command(exe, append([]string{"serve", "--stdio"}, args[1:]...), env, "")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	defer killTree(cmd.Process.Pid)
	lines := make(chan string, 1024)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 64<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	x := &exchange{lines: lines, answers: map[string]any{}, unmet: map[string]int{}}
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "wait "):
			x.talk.WriteString(line + "\n")
			method, needle, _ := strings.Cut(strings.TrimPrefix(line, "wait "), " ")
			if err := x.await("", method, needle); err != nil {
				return x.String(), err
			}
		default:
			sent := earlier.ReplaceAllStringFunc(line, func(ref string) string {
				parts := earlier.FindStringSubmatch(ref)
				return fmt.Sprint(lookup(x.answers[parts[1]], parts[2]))
			})
			var request wireLine
			if err := json.Unmarshal([]byte(sent), &request); err != nil {
				return x.String(), fmt.Errorf("%s: %w", line, err)
			}
			x.talk.WriteString("> " + sent + "\n")
			if _, err := io.WriteString(stdin, sent+"\n"); err != nil {
				return x.String(), err
			}
			if err := x.await(string(request.ID), "", ""); err != nil {
				return x.String(), err
			}
		}
	}
	_ = stdin.Close()
	exited := make(chan error, 1)
	go func() {
		for line := range lines {
			x.take(line)
		}
		exited <- cmd.Wait()
	}()
	select {
	case err = <-exited:
	case <-time.After(exitWithin):
		return x.String(), fmt.Errorf("serve did not exit within %s of its standard input closing", exitWithin)
	}
	return rendered(x.String(), stderr.String(), err)
}

func (x *exchange) String() string {
	return x.talk.String() + strings.Join(x.notes, "")
}

func (x *exchange) take(line string) wireLine {
	var got wireLine
	if json.Unmarshal([]byte(line), &got) != nil || got.Method == "" {
		x.talk.WriteString("< " + line + "\n")
		return got
	}
	x.unmet[got.Method]++
	if got.Method == timedSnapshot {
		return got
	}
	relabelled := ""
	var whole map[string]any
	if json.Unmarshal([]byte(line), &whole) == nil {
		if params, ok := whole["params"].(map[string]any); ok {
			for _, field := range racingFields {
				delete(params, field)
			}
			delete(params, racingByMethod[got.Method])
			if got.Method == labelBroadcast && params["turn"] == "" {
				relabelled = fmt.Sprint(params["item"])
			}
		}
		canonical, _ := json.Marshal(whole)
		line = string(canonical)
	}
	note := notificationMark + line + "\n"
	if slices.Contains(polledMethods, got.Method) && slices.Contains(x.notes, note) {
		return got
	}
	if relabelled != "" {
		x.notes = slices.DeleteFunc(x.notes, func(earlier string) bool {
			return strings.Contains(earlier, `"method":"`+labelBroadcast+`"`) && strings.Contains(earlier, `"item":"`+relabelled+`"`) && strings.Contains(earlier, `"turn":""`)
		})
	}
	x.notes = append(x.notes, note)
	return got
}

func (x *exchange) met(method, needle string) bool {
	if needle != "" {
		return slices.ContainsFunc(x.notes, func(note string) bool {
			return strings.Contains(note, `"method":"`+method+`"`) && strings.Contains(note, needle)
		})
	}
	if x.unmet[method] > 0 {
		x.unmet[method]--
		return true
	}
	return false
}

func (x *exchange) await(id, method, needle string) error {
	deadline := time.After(answerWithin)
	for {
		if method != "" && x.met(method, needle) {
			return nil
		}
		select {
		case line, open := <-x.lines:
			if !open {
				return fmt.Errorf("serve closed its output waiting for %s%s", id, method)
			}
			got := x.take(line)
			if id != "" && string(got.ID) == id && got.Method == "" {
				var whole any
				_ = json.Unmarshal([]byte(line), &whole)
				x.answers[id] = whole
				return nil
			}
		case <-deadline:
			return fmt.Errorf("no answer to %s%s within %s", id, method, answerWithin)
		}
	}
}

func lookup(value any, path string) any {
	for _, part := range strings.Split(path, ".") {
		switch node := value.(type) {
		case map[string]any:
			value = node[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index >= len(node) {
				return nil
			}
			value = node[index]
		default:
			return nil
		}
	}
	return value
}
