package cost

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	transcriptExt    = ".jsonl"
	metaExt          = ".meta.json"
	agentPrefix      = "agent-"
	exitCodeText     = "Exit code "
	tornWritePadding = "\x00 \t\r"
)

type text string

func (t *text) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '"' {
		return nil
	}
	return json.Unmarshal(raw, (*string)(t))
}

type exitCode bool

func (e *exitCode) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '[' {
		*e = exitCode(bytes.HasPrefix(raw, []byte(`"`+exitCodeText)))
		return nil
	}
	var blocks []struct {
		Text text `json:"text"`
	}
	err := json.Unmarshal(raw, &blocks)
	*e = exitCode(len(blocks) > 0 && strings.HasPrefix(string(blocks[0].Text), exitCodeText))
	return err
}

type input struct {
	Command     text `json:"command"`
	Prompt      text `json:"prompt"`
	Description text `json:"description"`
	Message     text `json:"message"`
	Summary     text `json:"summary"`
}

type block struct {
	Type      string   `json:"type"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Text      text     `json:"text"`
	Input     input    `json:"input"`
	ToolUseID string   `json:"tool_use_id"`
	IsError   bool     `json:"is_error"`
	Content   exitCode `json:"content"`
}

type content struct {
	text   text
	blocks []block
}

func (c *content) UnmarshalJSON(raw []byte) error {
	if len(raw) > 0 && raw[0] == '[' {
		return json.Unmarshal(raw, &c.blocks)
	}
	return c.text.UnmarshalJSON(raw)
}

type usage struct {
	In         int64 `json:"input_tokens"`
	Out        int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	Details    struct {
		Thinking int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (u usage) outranks(other usage) bool {
	a := [...]int64{u.Out, u.In, u.CacheRead, u.CacheWrite, u.Details.Thinking}
	b := [...]int64{other.Out, other.In, other.CacheRead, other.CacheWrite, other.Details.Thinking}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

type result struct {
	FilePath string `json:"filePath"`
	Type     string `json:"type"`
	Patch    []struct {
		Lines []string `json:"lines"`
	} `json:"structuredPatch"`
	Content text   `json:"content"`
	Resumed string `json:"resumedAgentId"`
	Revived string `json:"revivedAgentId"`
}

func (r *result) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	type plain result
	return json.Unmarshal(raw, (*plain)(r))
}

type line struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	ID         string `json:"id"`
	Timestamp  string `json:"timestamp"`
	IsMeta     bool   `json:"isMeta"`
	RequestRef string `json:"requestRef"`
	ThinkingMs int64  `json:"thinkingDurationMs"`
	Attachment struct {
		Type string `json:"type"`
	} `json:"attachment"`
	Message struct {
		ID      string  `json:"id"`
		Content content `json:"content"`
		Usage   *usage  `json:"usage"`
	} `json:"message"`
	Result result `json:"toolUseResult"`
}

type message struct {
	end   int64
	ref   string
	think int64
	usage usage
}

type call struct {
	name     string
	at       int64
	used     bool
	named    string
	command  string
	end      int64
	done     bool
	isError  bool
	exitCode bool
	added    int
	removed  int
	file     string
	created  bool
	resumed  string
}

type prompt struct {
	at     int64
	ticket string
}

type file struct {
	agent       string
	meta        meta
	ticks       []int64
	messages    map[string]*message
	requests    map[string]int64
	calls       map[string]*call
	prompts     []prompt
	compactions []int64
}

type meta struct {
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	ToolUseID   string `json:"toolUseId"`
}

type reader struct {
	ticket, test, build *regexp.Regexp
}

func newReader(keys []string) reader {
	quoted := make([]string, len(keys))
	for i, key := range keys {
		quoted[i] = regexp.QuoteMeta(key)
	}
	ticket := `\b(` + strings.Join(quoted, "|") + `)-\d+\b`
	if len(keys) == 0 {
		ticket = `[^\s\S]`
	}
	return reader{
		ticket: regexp.MustCompile(ticket),
		test:   regexp.MustCompile(`\b(go|bun)\s+test\b|\bcargo\s+(test|nextest)\b`),
		build:  regexp.MustCompile(`\bgo\s+(build|vet)\b|\b(tsc|bun build)\b|\bcargo\s+(build|check|clippy)\b`),
	}
}

func (rd reader) named(texts ...text) string {
	for _, said := range texts {
		if found := rd.ticket.FindString(string(said)); found != "" {
			return found
		}
	}
	return ""
}

func readAll(paths []string, rd reader) ([]*file, error) {
	read, failed := make([]*file, len(paths)), make([]error, len(paths))
	next := make(chan int, len(paths))
	for at := range paths {
		next <- at
	}
	close(next)
	var workers sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(paths)) {
		workers.Go(func() {
			for at := range next {
				read[at], failed[at] = rd.readFile(paths[at])
			}
		})
	}
	workers.Wait()
	return read, errors.Join(failed...)
}

func (rd reader) readFile(path string) (*file, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f := &file{messages: map[string]*message{}, requests: map[string]int64{}, calls: map[string]*call{}}
	if name := filepath.Base(path); strings.HasPrefix(name, agentPrefix) {
		f.agent = strings.TrimSuffix(strings.TrimPrefix(name, agentPrefix), transcriptExt)
		metaPath := strings.TrimSuffix(path, transcriptExt) + metaExt
		described, err := os.ReadFile(metaPath)
		if err == nil {
			err = json.Unmarshal(described, &f.meta)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", metaPath, err)
		}
	}
	for number := 1; ; number++ {
		end := bytes.IndexByte(body, '\n')
		if end < 0 {
			return f, nil
		}
		raw := bytes.Trim(body[:end], tornWritePadding)
		body = body[end+1:]
		if len(raw) == 0 || bytes.HasPrefix(raw, []byte(`{"type":"api-request-blob"`)) {
			continue
		}
		var l line
		err := json.Unmarshal(raw, &l)
		if err == nil {
			err = rd.add(f, l)
		}
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, number, err)
		}
	}
}

func (rd reader) add(f *file, l line) error {
	if l.Timestamp == "" {
		return nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return err
	}
	at := stamp.UnixMilli()
	f.ticks = append(f.ticks, at)
	switch l.Type {
	case "api-request":
		f.requests[l.ID] = at
	case "assistant":
		rd.assistant(f, l, at)
	case "user":
		rd.user(f, l, at)
	case "attachment":
		if l.Attachment.Type == "queued_command" {
			f.prompts = append(f.prompts, prompt{at: at})
		}
	case "system":
		switch l.Subtype {
		case "compact_boundary":
			f.compactions = append(f.compactions, at)
		case "scheduled_task_fire":
			f.prompts = append(f.prompts, prompt{at: at})
		}
	}
	return nil
}

func (f *file) call(id string) *call {
	c := f.calls[id]
	if c == nil {
		c = &call{}
		f.calls[id] = c
	}
	return c
}

func (rd reader) assistant(f *file, l line, at int64) {
	id := l.Message.ID
	if id == "" {
		return
	}
	m := f.messages[id]
	if m == nil {
		m = &message{end: at}
		f.messages[id] = m
	}
	m.end, m.think = max(m.end, at), m.think+l.ThinkingMs
	if l.RequestRef != "" {
		m.ref = l.RequestRef
	}
	if l.Message.Usage != nil && l.Message.Usage.outranks(m.usage) {
		m.usage = *l.Message.Usage
	}
	for _, used := range l.Message.Content.blocks {
		if used.Type != "tool_use" {
			continue
		}
		c := f.call(used.ID)
		c.name, c.at, c.used, c.command = used.Name, at, true, string(used.Input.Command)
		switch used.Name {
		case "Bash", "PowerShell":
			if strings.Contains(c.command, "boardy") {
				c.named = rd.named(used.Input.Command)
			}
		case "Agent", "Task":
			c.named = rd.named(used.Input.Prompt, used.Input.Description)
		case "SendMessage":
			c.named = rd.named(used.Input.Message, used.Input.Summary)
		}
	}
}

func (rd reader) user(f *file, l line, at int64) {
	results := false
	for _, answer := range l.Message.Content.blocks {
		if answer.Type != "tool_result" {
			continue
		}
		results = true
		c := f.call(answer.ToolUseID)
		c.end, c.done, c.isError, c.exitCode = at, true, answer.IsError, answer.IsError && bool(answer.Content)
		c.file, c.created, c.resumed = l.Result.FilePath, l.Result.Type == "create", max(l.Result.Resumed, l.Result.Revived)
		c.added, c.removed = 0, 0
		for _, hunk := range l.Result.Patch {
			for _, changed := range hunk.Lines {
				switch {
				case strings.HasPrefix(changed, "+"):
					c.added++
				case strings.HasPrefix(changed, "-"):
					c.removed++
				}
			}
		}
		if c.created && len(l.Result.Patch) == 0 {
			c.added += strings.Count(string(l.Result.Content), "\n") + 1
		}
	}
	if results || l.IsMeta {
		return
	}
	var said []text
	for _, part := range l.Message.Content.blocks {
		said = append(said, part.Text)
	}
	f.prompts = append(f.prompts, prompt{at: at, ticket: rd.named(append(said, l.Message.Content.text)...)})
}
