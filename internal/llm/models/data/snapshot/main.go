package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"tofu/internal/llm/models"
)

const (
	snapshotFile = "data/models-dev.json"
	takenFile    = "data/models-dev.taken"
	writtenMode  = 0o644
)

type provider struct {
	Models map[string]map[string]json.RawMessage `json:"models"`
}

func main() {
	if err := snapshot(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "snapshot:", err)
		os.Exit(1)
	}
}

func snapshot(vendors []string) error {
	if len(vendors) == 0 {
		return errors.New("name the models.dev providers to keep, as in go run ./data/snapshot anthropic openai meta")
	}
	source := models.RegistrySource()
	response, err := http.Get(source)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", source, response.Status)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	var served map[string]provider
	if err := json.Unmarshal(body, &served); err != nil {
		return err
	}
	kept := map[string]provider{}
	for _, vendor := range vendors {
		listed, found := served[vendor]
		if !found {
			return fmt.Errorf("%s lists no provider %q", source, vendor)
		}
		trimmed := provider{Models: map[string]map[string]json.RawMessage{}}
		for id, fields := range listed.Models {
			trimmed.Models[id] = map[string]json.RawMessage{}
			for _, field := range []string{"limit", "tool_call", "reasoning", "reasoning_options", "modalities", "cost"} {
				if value, set := fields[field]; set {
					trimmed.Models[id][field] = value
				}
			}
		}
		kept[vendor] = trimmed
	}
	written, err := json.MarshalIndent(kept, "", " ")
	if err != nil {
		return err
	}
	if _, err := models.ParseRegistry(written, snapshotFile); err != nil {
		return err
	}
	if err := os.WriteFile(snapshotFile, append(written, '\n'), writtenMode); err != nil {
		return err
	}
	return os.WriteFile(takenFile, []byte(time.Now().UTC().Format(time.DateOnly)+"\n"), writtenMode)
}
