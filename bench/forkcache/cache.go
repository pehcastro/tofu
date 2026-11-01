package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
)

const (
	bytesPerThousandTokens = 2310
	lookbackBlocks         = 20
	inputWeight            = 1.0
	readWeight             = 0.1
	write5mWeight          = 1.25
	write1hWeight          = 2.0
	ttlOneHour             = "1h"
	ttlFiveMinutes         = "5m"
)

var billingCheck = regexp.MustCompile(`cch=[0-9a-f]+`)

type block struct {
	bytes      int
	breakpoint string
	prefix     [32]byte
}

type usage struct {
	read, write5m, write1h, input int
}

func (u usage) written() int { return u.write5m + u.write1h }

func (u usage) plus(o usage) usage {
	return usage{read: u.read + o.read, write5m: u.write5m + o.write5m, write1h: u.write1h + o.write1h, input: u.input + o.input}
}

func (u usage) total() int { return u.read + u.written() + u.input }

func (u usage) cost() float64 {
	return inputWeight*float64(u.input) + readWeight*float64(u.read) + write5mWeight*float64(u.write5m) + write1hWeight*float64(u.write1h)
}

func tokens(bytes int) int { return bytes * 1000 / bytesPerThousandTokens }

type wireBody struct {
	Tools    []map[string]any `json:"tools"`
	System   []map[string]any `json:"system"`
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	ToolChoice map[string]any `json:"tool_choice"`
}

func (b wireBody) choice() string {
	if b.ToolChoice == nil {
		return `{"type":"auto"}`
	}
	text, _ := json.Marshal(b.ToolChoice)
	return string(text)
}

func blocksOf(body wireBody) []block {
	var blocks []block
	var running [32]byte
	add := func(role string, part map[string]any) {
		ttl := ""
		if control, marked := part["cache_control"].(map[string]any); marked {
			ttl, _ = control["ttl"].(string)
			ttl = cmp.Or(ttl, ttlFiveMinutes)
			delete(part, "cache_control")
		}
		text, _ := json.Marshal(part)
		canonical := role + billingCheck.ReplaceAllString(string(text), "cch=0")
		running = sha256.Sum256(append(running[:], canonical...))
		blocks = append(blocks, block{bytes: len(canonical), breakpoint: ttl, prefix: running})
	}
	for _, tool := range body.Tools {
		add("tool", tool)
	}
	for _, system := range body.System {
		add("system", system)
	}
	for _, message := range body.Messages {
		var content []map[string]any
		if json.Unmarshal(message.Content, &content) != nil {
			var text string
			_ = json.Unmarshal(message.Content, &text)
			content = []map[string]any{{"type": "text", "text": text}}
		}
		for _, part := range content {
			add(message.Role, part)
		}
	}
	return blocks
}

func sharedTokens(before, after []block) int {
	shared, bytes := 0, 0
	for ; shared < min(len(before), len(after)) && before[shared].prefix == after[shared].prefix; shared++ {
		bytes += before[shared].bytes
	}
	return tokens(bytes)
}

type reading struct {
	readBlocks   int
	cachedTokens int
	reach        int
}

func (r reading) pastLookback() bool { return r.reach > lookbackBlocks }

func (r reading) columns() []string {
	if r.reach < 0 {
		return []string{fmt.Sprint(r.cachedTokens), "none", "ok"}
	}
	check := "ok"
	if r.pastLookback() {
		check = "fail"
	}
	return []string{fmt.Sprint(r.cachedTokens), fmt.Sprint(r.reach), check}
}

type promptCache map[[32]byte]bool

func (c promptCache) bill(blocks []block) (usage, reading) {
	last, readTo, cachedTo := -1, 0, 0
	for at, b := range blocks {
		if c[b.prefix] {
			cachedTo = at + 1
		}
		if b.breakpoint == "" {
			continue
		}
		last = at
		for back := at; back >= max(0, at-lookbackBlocks); back-- {
			if c[blocks[back].prefix] {
				readTo = max(readTo, back+1)
				break
			}
		}
	}
	look := reading{readBlocks: readTo, reach: -1}
	for at := cachedTo - 1; cachedTo > 0 && at < len(blocks) && look.reach < 0; at++ {
		if blocks[at].breakpoint != "" {
			look.reach = at - (cachedTo - 1)
		}
	}
	readBytes, cachedBytes, inputBytes, ttl := 0, 0, 0, ""
	written := map[string]int{}
	for at := len(blocks) - 1; at >= 0; at-- {
		b := blocks[at]
		if at < cachedTo {
			cachedBytes += b.bytes
		}
		if b.breakpoint != "" {
			ttl = b.breakpoint
			c[b.prefix] = true
		}
		switch {
		case at < readTo:
			readBytes += b.bytes
		case at <= last:
			written[ttl] += b.bytes
		default:
			inputBytes += b.bytes
		}
	}
	look.cachedTokens = tokens(cachedBytes)
	return usage{read: tokens(readBytes), input: tokens(inputBytes), write1h: tokens(written[ttlOneHour]), write5m: tokens(written[ttlFiveMinutes])}, look
}
