package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/openrouter"
	"tofu/internal/judge/jev/wire/typesafe"
)

const (
	side            = 850
	reference       = "f3b2c9d4-7e1a-4c55-9a0e-probe850"
	imagesHeader    = "TypeSafe-Danger-Zone-Images"
	ownerTokens     = 2381
	dollarsPerToken = 0.042 / 1e6
)

type endpoint struct {
	wire, url, model, variable string
}

var endpoints = []endpoint{
	{"typesafe", typesafe.Endpoint, "jev-latest", jev.TypeSafeVariable},
	{"openrouter", openrouter.Endpoint, "~typesafe/jev-latest", jev.OpenRouterVariable},
}

func picture() []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			shade := color.RGBA{R: 245, G: 245, B: 245, A: 255}
			if x > 300 && x < 550 && y > 300 && y < 550 {
				shade = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			}
			canvas.Set(x, y, shade)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		panic(err)
	}
	return out.Bytes()
}

type answer struct {
	status  int
	elapsed time.Duration
	model   string
	input   int
	cost    float64
	said    string
}

func post(ctx context.Context, at endpoint, key string, shot []byte) (answer, error) {
	payload := map[string]any{
		"model": at.model,
		"state": map[string]string{"screen": reference},
		"questions": map[string]any{"red_square": map[string]any{
			"type": "noul", "instructions": "Does `screen` show a red square?",
			"criteria": map[string]string{"true": "a red square is shown", "false": "no red square is shown"},
		}},
	}
	if shot != nil {
		payload["assets"] = map[string]any{reference: map[string]string{"type": "image", "format": "png", "data_base64": base64.StdEncoding.EncodeToString(shot)}}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return answer{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, at.url, bytes.NewReader(encoded))
	if err != nil {
		return answer{}, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	if shot != nil {
		request.Header.Set(imagesHeader, "true")
	}
	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return answer{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return answer{}, err
	}
	var decoded struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens int     `json:"input_tokens"`
			Cost        float64 `json:"cost"`
		} `json:"usage"`
		Answers json.RawMessage `json:"answers"`
	}
	got := answer{status: response.StatusCode, elapsed: time.Since(started)}
	if json.Unmarshal(raw, &decoded) != nil || response.StatusCode != http.StatusOK {
		got.said = strings.Join(strings.Fields(string(raw)), " ")
		return got, nil
	}
	got.model, got.input, got.cost, got.said = decoded.Model, decoded.Usage.InputTokens, decoded.Usage.Cost, string(decoded.Answers)
	return got, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	machine, _ := os.Hostname()
	shot := picture()
	fmt.Printf("image probe, %s, %s, one %dx%d PNG of %d bytes in state, referred to once, against the owner figure of %d tokens\n\n",
		machine, time.Now().Format(time.DateOnly), side, side, len(shot), ownerTokens)
	fmt.Println("| wire | arm | http | build | input tokens | wire cost | wall | image tokens | image dollars | answer or refusal |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|---|")
	for _, at := range endpoints {
		key, err := jev.KeyFor(".env", at.variable)
		if err != nil {
			fmt.Printf("| %s | skipped | | | | | | | | no credential: %v |\n", at.wire, err)
			continue
		}
		control, err := post(ctx, at, key, nil)
		if err != nil {
			fmt.Printf("| %s | off | | | | | | | | %v |\n", at.wire, err)
			continue
		}
		fmt.Printf("| %s | off, text reference only | %d | %s | %d | $%.7f | %d ms | | | %s |\n", at.wire, control.status, control.model, control.input, control.cost, control.elapsed.Milliseconds(), control.said)
		pictured, err := post(ctx, at, key, shot)
		if err != nil {
			fmt.Printf("| %s | on | | | | | | | | %v |\n", at.wire, err)
			continue
		}
		extra := pictured.input - control.input
		fmt.Printf("| %s | on, image and header | %d | %s | %d | $%.7f | %d ms | %d | $%.7f | %s |\n", at.wire, pictured.status, pictured.model, pictured.input, pictured.cost, pictured.elapsed.Milliseconds(), extra, float64(extra)*dollarsPerToken, pictured.said)
	}
}
