package transform

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	tf "boji/internal/transform"
)

type Estimate struct {
	TokensPerByte float64
	StepOverhead  float64
	Points        int
	WorstResidual float64
}

type point struct{ bytes, tokens float64 }

func FitTokens(writes []Write) Estimate {
	var points []point
	var sumX, sumY, sumXX, sumXY float64
	for _, write := range writes {
		if !write.AloneInStep {
			continue
		}
		at := point{float64(len(write.Content) + len(write.Path)), float64(write.CompletionTokens)}
		points = append(points, at)
		sumX, sumY = sumX+at.bytes, sumY+at.tokens
		sumXX, sumXY = sumXX+at.bytes*at.bytes, sumXY+at.bytes*at.tokens
	}
	n := float64(len(points))
	slope := (n*sumXY - sumX*sumY) / (n*sumXX - sumX*sumX)
	fit := Estimate{TokensPerByte: slope, StepOverhead: (sumY - slope*sumX) / n, Points: len(points)}
	for _, at := range points {
		fit.WorstResidual = math.Max(fit.WorstResidual,
			math.Abs(at.tokens-(fit.StepOverhead+slope*at.bytes)))
	}
	return fit
}

func (e Estimate) Tokens(bytes int) int {
	return int(math.Round(e.TokensPerByte * float64(bytes)))
}

type Row struct {
	Write
	Shape       string
	Edits       int
	Expressible bool
	Why         string
	WriteOut    int
	WriteIn     int
	TypedOut    int
	TypedIn     int
}

type writeCall struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type typedCall struct {
	Path  string    `json:"path"`
	Edits []tf.Edit `json:"edits"`
}

func Measure(writes []Write, root string) ([]Row, error) {
	rows := make([]Row, 0, len(writes))
	for i, write := range writes {
		call, err := json.Marshal(writeCall{Path: write.Path, Content: write.Content})
		if err != nil {
			return nil, fmt.Errorf("serialising the write call for %s: %w", write.Path, err)
		}
		row := Row{Write: write, WriteOut: len(call), WriteIn: write.ResultBytes}
		edits, preview, err := rebuild(filepath.Join(root, strconv.Itoa(i)), write)
		if err != nil {
			row.Why = err.Error()
			rows = append(rows, row)
			continue
		}
		typed, err := json.Marshal(typedCall{Path: write.Path, Edits: edits})
		if err != nil {
			return nil, fmt.Errorf("serialising the typed call for %s: %w", write.Path, err)
		}
		shape := tf.Replace
		if len(edits) == 1 && edits[0].Kind == tf.Create {
			shape = tf.Create
		}
		row.Expressible, row.Shape, row.Edits = true, string(shape), len(edits)
		row.TypedOut, row.TypedIn = len(typed), len(preview.Result())
		rows = append(rows, row)
	}
	return rows, nil
}

func rebuild(root string, write Write) ([]tf.Edit, tf.Preview, error) {
	edits, err := tf.Derive(write.Before, write.Content)
	if err != nil {
		return nil, tf.Preview{}, err
	}
	local := filepath.FromSlash(write.Path)
	full := filepath.Join(root, local)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return nil, tf.Preview{}, fmt.Errorf("making the rebuild directory for %s: %w", write.Path, err)
	}
	if write.Before != "" {
		if err := os.WriteFile(full, []byte(write.Before), 0o600); err != nil {
			return nil, tf.Preview{}, fmt.Errorf("seeding %s: %w", write.Path, err)
		}
	}
	preview, err := tf.Plan(root, local, edits)
	if err != nil {
		return nil, tf.Preview{}, err
	}
	if err := tf.Commit(root, preview); err != nil {
		return nil, tf.Preview{}, err
	}
	rebuilt, err := os.ReadFile(full)
	if err != nil {
		return nil, tf.Preview{}, fmt.Errorf("reading the rebuilt %s: %w", write.Path, err)
	}
	if string(rebuilt) != write.Content {
		return nil, tf.Preview{}, fmt.Errorf("the typed edits rebuilt %d bytes where the recorded write held %d",
			len(rebuilt), len(write.Content))
	}
	return edits, preview, nil
}
