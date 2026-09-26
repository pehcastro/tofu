package instruction

import (
	"errors"
	"path/filepath"

	"tofu/bench/corpus"
)

type Row struct {
	Turn              string
	Key               string
	Tool              string
	Task              string
	Content           string
	InstructionShaped bool
}

type Corpus struct {
	Dir     string
	Rows    []Row
	Skipped []string
}

type outsideText struct {
	Tool    string
	Content string
}

type turnRead struct {
	messages map[string]outsideText
	task     string
	err      error
}

func readTurnMessages(dir, turn string) (map[string]outsideText, string, error) {
	recorded, err := corpus.ReadTurnDir(filepath.Join(dir, turn))
	if err != nil && !errors.Is(err, corpus.ErrNoWallClock) {
		return nil, "", err
	}
	names := map[string]string{}
	messages := map[string]outsideText{}
	for _, message := range recorded.Messages {
		switch message.Role {
		case "assistant":
			for _, call := range message.ToolCalls {
				names[call.ID] = call.Name
			}
		case "tool":
			if message.ToolCallID == "" || message.Content == "" {
				continue
			}
			messages[message.ToolCallID] = outsideText{Tool: names[message.ToolCallID], Content: message.Content}
		}
	}
	return messages, recorded.Task, nil
}

func Load(dir string) (Corpus, error) {
	loaded := Corpus{Dir: dir}
	cache := map[string]turnRead{}
	for _, l := range labels {
		read, cached := cache[l.Turn]
		if !cached {
			messages, task, err := readTurnMessages(dir, l.Turn)
			read = turnRead{messages: messages, task: task, err: err}
			cache[l.Turn] = read
		}
		if read.err != nil {
			loaded.Skipped = append(loaded.Skipped, l.Turn+" "+l.Key+": "+read.err.Error())
			continue
		}
		msg, ok := read.messages[l.Key]
		if !ok {
			loaded.Skipped = append(loaded.Skipped, l.Turn+" "+l.Key+": the recorded tool call is not in the session today")
			continue
		}
		if leaks := corpus.LeaksIn(msg.Content + " " + read.task); len(leaks) > 0 {
			loaded.Skipped = append(loaded.Skipped, l.Turn+" "+l.Key+": the scrub left something behind, skipped rather than printed")
			continue
		}
		loaded.Rows = append(loaded.Rows, Row{
			Turn: l.Turn, Key: l.Key, Tool: msg.Tool, Task: read.task,
			Content: msg.Content, InstructionShaped: l.InstructionShaped,
		})
	}
	return loaded, nil
}
