package motion

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Frame struct {
	File             string   `json:"file"`
	ChromeTimestampS float64  `json:"chromeTimestampS"`
	MsFromTrigger    *float64 `json:"msFromTrigger"`
}

type Element struct {
	X          float64           `json:"x"`
	Y          float64           `json:"y"`
	Width      float64           `json:"width"`
	Height     float64           `json:"height"`
	Opacity    float64           `json:"opacity"`
	Display    string            `json:"display"`
	Visibility string            `json:"visibility"`
	Hidden     bool              `json:"hidden"`
	Styles     map[string]string `json:"styles,omitempty"`
}

type Sample struct {
	MsFromTrigger *float64            `json:"msFromTrigger"`
	Elements      map[string]*Element `json:"elements"`
}

type Trigger struct {
	Event  string  `json:"event"`
	WallMs float64 `json:"wallMs"`
}

type App struct {
	Revision   string `json:"revision"`
	DiffSHA256 string `json:"diffSha256"`
}

type Manifest struct {
	TakeID           string   `json:"takeId"`
	Scenario         Scenario `json:"scenario"`
	Trigger          *Trigger `json:"trigger"`
	Browser          string   `json:"browser"`
	App              App      `json:"app"`
	FrameCount       int      `json:"frameCount"`
	TraceSampleCount int      `json:"traceSampleCount"`
}

type Take struct {
	Dir      string
	Manifest Manifest
	Frames   []Frame
	Trace    []Sample
}

func notIDLetter(r rune) bool {
	return (r < 'a' || r > 'z') && (r < '0' || r > '9')
}

func NewID(now time.Time, label string, n int) string {
	label = strings.Join(strings.FieldsFunc(strings.ToLower(label), notIDLetter), "-")
	if label == "" {
		label = "take"
	}
	suffix := make([]byte, 2)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s-%s-%d-%s", now.UTC().Format("20060102T150405"), label, n, hex.EncodeToString(suffix))
}

func takeDir(root, id string) (string, error) {
	if id == "" || strings.IndexFunc(strings.ToLower(id), func(r rune) bool { return r != '-' && notIDLetter(r) }) >= 0 {
		return "", fmt.Errorf("not a take id: %q", id)
	}
	return filepath.Join(root, id), nil
}

func Save(root string, t Take, jpegs [][]byte) (Take, error) {
	dir, err := takeDir(root, t.Manifest.TakeID)
	if err != nil {
		return t, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		return t, err
	}
	for i, data := range jpegs {
		t.Frames[i].File = fmt.Sprintf("frames/%06d.jpg", i+1)
		if err := os.WriteFile(filepath.Join(dir, t.Frames[i].File), data, 0o644); err != nil {
			return t, err
		}
	}
	t.Dir = dir
	t.Manifest.FrameCount, t.Manifest.TraceSampleCount = len(t.Frames), len(t.Trace)
	for name, v := range map[string]any{"frames.json": t.Frames, "trace.json": t.Trace, "manifest.json": t.Manifest} {
		data, err := json.Marshal(v)
		if err != nil {
			return t, err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return t, err
		}
	}
	return t, nil
}

func Load(root, id string) (Take, error) {
	dir, err := takeDir(root, id)
	if err != nil {
		return Take{}, err
	}
	t := Take{Dir: dir}
	for name, v := range map[string]any{"manifest.json": &t.Manifest, "frames.json": &t.Frames, "trace.json": &t.Trace} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return Take{}, fmt.Errorf("take %s: %w", id, err)
		}
		if err := json.Unmarshal(data, v); err != nil {
			return Take{}, fmt.Errorf("take %s: %s: %w", id, name, err)
		}
	}
	return t, nil
}
