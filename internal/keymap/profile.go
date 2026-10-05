package keymap

import (
	"embed"
	"encoding/json"
	"strings"
)

//go:embed profiles/*.json schema.json
var catalog embed.FS

type Binding struct {
	Key    string `json:"key"`
	Owner  string `json:"owner"`
	Status string `json:"status"`
}

type Profile struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Layer      string    `json:"layer"`
	Source     string    `json:"source"`
	Inspection string    `json:"inspection"`
	Bindings   []Binding `json:"bindings"`
}

type Conflict struct {
	Action  string
	Key     string
	Binding Binding
}

func Lookup(id string) (Profile, bool) {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return Profile{}, false
	}
	data, err := catalog.ReadFile("profiles/" + id + ".json")
	if err != nil {
		return Profile{}, false
	}
	var p Profile
	if json.Unmarshal(data, &p) != nil || p.ID != id || p.Name == "" || p.Source == "" {
		return Profile{}, false
	}
	return p, true
}

func Conflicts(profile Profile, shortcuts map[string]string) []Conflict {
	var found []Conflict
	for action, key := range shortcuts {
		for _, binding := range profile.Bindings {
			if key != "" && strings.EqualFold(key, binding.Key) {
				found = append(found, Conflict{action, key, binding})
			}
		}
	}
	return found
}
