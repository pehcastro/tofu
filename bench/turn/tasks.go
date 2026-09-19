package turn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Task struct {
	Name   string
	Prompt string
	Setup  func(dir string) error
	Check  func(dir string) (bool, string)
}

func Tasks() []Task {
	return []Task{
		{
			Name: "hello",
			Prompt: "Using the write tool, write a file named hello.txt containing exactly the text " +
				"hello from boji and nothing else. After it is written, reply with a short " +
				"confirmation message in plain text and do not call any more tools.",
			Check: checkExactFile("hello.txt", "hello from boji"),
		},
		{
			Name: "config",
			Prompt: "Using the write tool, create a file named config.json containing a JSON object " +
				"with exactly two keys: name set to the string boji and version set to the number 1. " +
				"Reply with a short confirmation message in plain text after it is written and do not " +
				"call any more tools.",
			Check: checkConfigJSON,
		},
		{
			Name: "twofiles",
			Prompt: "Using the write tool, create two files in this directory: notes.txt containing " +
				"exactly the text draft, and status.txt containing exactly the text done. Reply with " +
				"a short confirmation message in plain text after both are written and do not call " +
				"any more tools.",
			Check: checkTwoFiles,
		},
		{
			Name: "double",
			Prompt: "This directory already contains a file named seed.txt holding a single integer. " +
				"Using the read tool, read seed.txt, then using the write tool, create a file named " +
				"result.txt containing exactly the doubled value of that integer as plain text and " +
				"nothing else. Reply with a short confirmation message in plain text after result.txt " +
				"is written and do not call any more tools.",
			Setup: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("21"), 0o644)
			},
			Check: checkExactFile("result.txt", "42"),
		},
	}
}

func checkExactFile(name, want string) func(dir string) (bool, string) {
	return func(dir string) (bool, string) {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return false, fmt.Sprintf("%s: %v", name, err)
		}
		got := strings.TrimSpace(string(content))
		if got != want {
			return false, fmt.Sprintf("%s held %q, wanted %q", name, got, want)
		}
		return true, ""
	}
}

func checkTwoFiles(dir string) (bool, string) {
	if ok, why := checkExactFile("notes.txt", "draft")(dir); !ok {
		return false, why
	}
	if ok, why := checkExactFile("status.txt", "done")(dir); !ok {
		return false, why
	}
	return true, ""
}

func checkConfigJSON(dir string) (bool, string) {
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return false, fmt.Sprintf("config.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false, fmt.Sprintf("config.json is not valid JSON: %v", err)
	}
	if len(m) != 2 {
		return false, fmt.Sprintf("config.json has %d keys, wanted 2", len(m))
	}
	if m["name"] != "boji" {
		return false, fmt.Sprintf("config.json name was %v, wanted \"boji\"", m["name"])
	}
	if version, ok := m["version"].(float64); !ok || version != 1 {
		return false, fmt.Sprintf("config.json version was %v, wanted 1", m["version"])
	}
	return true, ""
}
