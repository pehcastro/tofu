package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	seedTurns        = 20
	seedStepsPerTurn = 10
	seedReadsPerStep = 4
)

type cassetteReply struct {
	Text  string         `json:"text"`
	Tools []cassetteTool `json:"tools,omitempty"`
}

type cassetteTool struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func mockFile(step, read int) string {
	return fmt.Sprintf("mock%02d_%d.go", step, read)
}

func seedCorpus(tofu, home, project string, env []string) error {
	for step := range seedStepsPerTurn {
		for read := range seedReadsPerStep {
			body := fmt.Sprintf("package mock\n\nfunc mock%d%d(input string) string { return input + %q }\n", step, read, "mock suffix")
			if err := os.WriteFile(filepath.Join(project, mockFile(step, read)), []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	var cassette, script bytes.Buffer
	encoder := json.NewEncoder(&cassette)
	for turn := range seedTurns {
		for step := range seedStepsPerTurn {
			reply := cassetteReply{Text: fmt.Sprintf("turn %d step %d, reading four mock files", turn, step)}
			for read := range seedReadsPerStep {
				reply.Tools = append(reply.Tools, cassetteTool{Name: "read", Args: map[string]any{"path": mockFile(step, read)}})
			}
			if err := encoder.Encode(reply); err != nil {
				return err
			}
		}
		if err := encoder.Encode(cassetteReply{Text: fmt.Sprintf("turn %d done: every mock file returns its input with a fixed suffix", turn)}); err != nil {
			return err
		}
		fmt.Fprintf(&script, "type what does every mock file return, pass %d\nkey enter\nwait turn %d done\n", turn, turn)
	}
	cassettePath, scriptPath := filepath.Join(home, "seed.cassette"), filepath.Join(home, "seed.drive")
	if err := os.WriteFile(cassettePath, cassette.Bytes(), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(scriptPath, script.Bytes(), 0o644); err != nil {
		return err
	}
	drive := exec.Command(tofu, "drive", scriptPath, "--home", home, "--cassette", cassettePath, "--plain", "--timeout", "120s", "--no-gate", "--sift", "free")
	drive.Dir, drive.Env = project, env
	if out, err := drive.CombinedOutput(); err != nil {
		return fmt.Errorf("seeding drive: %v\n%s", err, out[max(0, len(out)-4000):])
	}
	return errors.Join(os.Remove(cassettePath), os.Remove(scriptPath))
}
