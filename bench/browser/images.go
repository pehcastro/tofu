package browserbench

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tofu/internal/browser"
	"tofu/internal/judge/jev"
	"tofu/internal/judge/jev/wire/typesafe"
	"tofu/internal/judge/question"
)

const (
	ImagesHeader   = "TypeSafe-Danger-Zone-Images"
	BoxModel       = "jev-1.14.0"
	shotReference  = "tofu-bench-shot-7f3a91c2"
	boxQuestion    = "target"
	imageCallLimit = 30 * time.Second
)

type Rect struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

type Shot struct {
	ID          string          `json:"id"`
	Source      string          `json:"source"`
	SourceSHA   string          `json:"source_sha256"`
	Image       string          `json:"image"`
	ImageSHA    string          `json:"image_sha256"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Viewport    [2]int          `json:"viewport"`
	Rects       map[string]Rect `json:"rects"`
	Matches     bool            `json:"matches_recorded_page"`
	Differences []string        `json:"differences,omitempty"`
}

type Shots struct {
	Date     string `json:"date"`
	Machine  string `json:"machine"`
	Browser  string `json:"browser"`
	Recorder string `json:"recorder"`
	Viewport string `json:"viewport"`
	Frame    string `json:"frame"`
	Pages    []Shot `json:"pages"`
}

func LoadShots(dir string) (Shots, map[string][]byte, error) {
	var shots Shots
	raw, err := os.ReadFile(filepath.Join(dir, "shots.json"))
	if err != nil {
		return shots, nil, err
	}
	if err := json.Unmarshal(raw, &shots); err != nil {
		return shots, nil, fmt.Errorf("shots.json: %w", err)
	}
	images := map[string][]byte{}
	for _, shot := range shots.Pages {
		if images[shot.ID], err = os.ReadFile(filepath.Join(dir, shot.Image)); err != nil {
			return shots, nil, err
		}
	}
	return shots, images, nil
}

func postImages(ctx context.Context, key string, body []byte) (jev.Raw, error) {
	ctx, cancel := context.WithTimeout(ctx, imageCallLimit)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, typesafe.Endpoint, bytes.NewReader(body))
	if err != nil {
		return jev.Raw{}, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(ImagesHeader, "true")
	started := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return jev.Raw{}, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	elapsed := time.Since(started)
	if err == nil && response.StatusCode != http.StatusOK {
		err = fmt.Errorf("typesafe answered %d: %s", response.StatusCode, strings.Join(strings.Fields(string(raw)), " "))
	}
	return jev.Raw{Body: raw, Attempts: 1, Latency: elapsed, RequestID: response.Header.Get(typesafe.RequestIDHeader)}, err
}

func assets(image []byte) map[string]any {
	return map[string]any{shotReference: map[string]string{"type": "image", "format": "jpg", "data_base64": base64.StdEncoding.EncodeToString(image)}}
}

func withShot(body, image []byte) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(request["state"], &state); err != nil {
		return nil, fmt.Errorf("the state is not an object: %w", err)
	}
	state["screenshot"], _ = json.Marshal(shotReference)
	var err error
	if request["state"], err = json.Marshal(state); err != nil {
		return nil, err
	}
	if request["assets"], err = json.Marshal(assets(image)); err != nil {
		return nil, err
	}
	return json.Marshal(request)
}

type shotWire struct {
	jev.Wire
	key   string
	image []byte
}

func (w shotWire) Post(ctx context.Context, body []byte) (jev.Raw, error) {
	pictured, err := withShot(body, w.image)
	if err != nil {
		return jev.Raw{}, err
	}
	return postImages(ctx, w.key, pictured)
}

type Boxed struct {
	ID      string          `json:"id"`
	Element int             `json:"element"`
	Body    json.RawMessage `json:"body"`
	Latency time.Duration   `json:"latency_ns"`
	Error   string          `json:"error,omitempty"`
}

type ImageRecording struct {
	Conditions Conditions `json:"conditions"`
	Shots      string     `json:"shots"`
	Pictured   Recording  `json:"pictured"`
	BoxModel   string     `json:"box_model"`
	Boxes      []Boxed    `json:"boxes"`
}

func boxInstructions(c Case, target browser.Element) string {
	return fmt.Sprintf("the %s labelled %q in `screenshot` that a person acts on for this goal: %s", target.Role, target.Label, c.Goal)
}

func RecordImages(ctx context.Context, wire jev.Wire, key string, set question.Set, cases []Case, images map[string][]byte, shotsFile string, conditions Conditions) (ImageRecording, error) {
	recording := ImageRecording{Conditions: conditions, Shots: shotsFile, Pictured: Recording{Conditions: conditions, Model: wire.Model(), Caps: wire.Caps()}, BoxModel: BoxModel}
	for _, c := range cases {
		image, shot := images[c.ID]
		if !shot {
			return recording, fmt.Errorf("no screenshot for %s", c.ID)
		}
		recording.Pictured.Answers = append(recording.Pictured.Answers, Record(ctx, shotWire{Wire: wire, key: key, image: image}, set, []Case{c}, conditions).Answers...)
		target, targeted := c.Page.Element(c.Want.Element)
		if !targeted {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"model":     BoxModel,
			"state":     map[string]string{"screenshot": shotReference},
			"assets":    assets(image),
			"questions": map[string]any{boxQuestion: map[string]string{"type": "box", "instructions": boxInstructions(c, target)}},
		})
		if err != nil {
			return recording, err
		}
		raw, err := postImages(ctx, key, body)
		boxed := Boxed{ID: c.ID, Element: c.Want.Element, Body: raw.Body, Latency: raw.Latency}
		if err != nil {
			boxed.Error, boxed.Body = err.Error(), nil
		}
		recording.Boxes = append(recording.Boxes, boxed)
	}
	return recording, nil
}

type BoxReading struct {
	Case       Case
	Label      string
	Rect       Rect
	Box        Rect
	Inside     bool
	Confidence float64
	Input      int
	Latency    time.Duration
	Error      string
}

func ReadBoxes(cases []Case, shots Shots, recording ImageRecording) ([]BoxReading, error) {
	byID, shotOf := map[string]Case{}, map[string]Shot{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	for _, shot := range shots.Pages {
		shotOf[shot.ID] = shot
	}
	readings := make([]BoxReading, 0, len(recording.Boxes))
	for _, boxed := range recording.Boxes {
		c, shot := byID[boxed.ID], shotOf[boxed.ID]
		target, _ := c.Page.Element(boxed.Element)
		rect, measured := shot.Rects[strconv.Itoa(boxed.Element)]
		if !measured {
			return nil, fmt.Errorf("%s: shots.json has no rect for element %d", boxed.ID, boxed.Element)
		}
		width, height := float64(shot.Viewport[0]), float64(shot.Viewport[1])
		reading := BoxReading{Case: c, Label: target.Label, Latency: boxed.Latency, Error: boxed.Error,
			Rect: Rect{Left: rect.Left / width, Top: rect.Top / height, Right: rect.Right / width, Bottom: rect.Bottom / height}}
		if boxed.Error == "" {
			var answered struct {
				Answers map[string]struct {
					Box        Rect    `json:"box"`
					Confidence float64 `json:"confidence"`
				} `json:"answers"`
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(boxed.Body, &answered); err != nil {
				return nil, fmt.Errorf("%s: the box answer is not JSON: %w", boxed.ID, err)
			}
			answer := answered.Answers[boxQuestion]
			x, y := (answer.Box.Left+answer.Box.Right)/2, (answer.Box.Top+answer.Box.Bottom)/2
			reading.Box, reading.Confidence, reading.Input = answer.Box, answer.Confidence, answered.Usage.InputTokens
			reading.Inside = x >= reading.Rect.Left && x <= reading.Rect.Right && y >= reading.Rect.Top && y <= reading.Rect.Bottom
		}
		readings = append(readings, reading)
	}
	return readings, nil
}

func WriteImages(w io.Writer, shots Shots, before, text, pictured []Reading, beforeRecording, textRecording Recording, images ImageRecording, boxes []BoxReading, files []string) error {
	var b strings.Builder
	conditions := images.Conditions
	fmt.Fprintf(&b, "# bench browser, images: %s\n\n", conditions.Date)
	fmt.Fprintf(&b, "Machine: %s. Date: %s. Credential kind: %s. Wire for arms a, b and c: typesafe direct, with the header `%s: true` on b and c only. Box model: `%s`.\n\n", conditions.Machine, conditions.Date, conditions.Credential, ImagesHeader, images.BoxModel)
	fmt.Fprintf(&b, "Rebuilt from %s with no network call. Every page was asked once per arm. No call was discarded as a warm up. The live pass needs the network and `TOFU_LIVE=1`, and is skipped without it.\n\n", strings.Join(files, ", "))
	fmt.Fprintf(&b, "Screenshots: %s, recorded %s on %s by %s. Viewport %s; frame: %s. Browser: %s.\n\n", images.Shots, shots.Date, shots.Machine, shots.Recorder, shots.Viewport, shots.Frame, shots.Browser)
	b.WriteString("Arm b is arm a's request with one field added to the state, `screenshot`, holding the image reference once; the instructions are unchanged and never name the image. Dollars are input tokens at $0.042 per million, output free.\n\n")

	b.WriteString("## The chooser, tool against tool\n\n")
	b.WriteString(chooserHeader)
	beforeSummary, textSummary := summarise(before), summarise(text)
	b.WriteString(beforeSummary.row(fmt.Sprintf("before: page text, %s, %s, %s", beforeRecording.Caps.Name, strings.Join(beforeSummary.builds, ", "), beforeRecording.Conditions.Date)))
	b.WriteString(textSummary.row(fmt.Sprintf("a: page text only, as shipped, %s, %s", textRecording.Caps.Name, strings.Join(textSummary.builds, ", "))))
	pictureSummary := summarise(pictured)
	b.WriteString(pictureSummary.row(fmt.Sprintf("b: page text plus a screenshot in state, %s, %s", images.Pictured.Caps.Name, strings.Join(pictureSummary.builds, ", "))))

	b.WriteString("\n## Per page, arms a and b\n\n")
	b.WriteString("| page | want | a got | b got | a input | b input | b image tokens | a wall | b wall | matches recorded page | b error |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	matched := map[string]bool{}
	for _, shot := range shots.Pages {
		matched[shot.ID] = shot.Matches
	}
	for i, r := range pictured {
		a := text[i]
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %d | %d | %.0f ms | %.0f ms | %t | %s |\n",
			r.Case.ID, action(r.Case.Want), got(a), got(r), a.Input, r.Input, r.Input-a.Input, millis(a.Wall), millis(r.Wall), matched[r.Case.ID], r.Error)
	}

	inside, clicks, clicksInside := 0, 0, 0
	for _, r := range boxes {
		if r.Inside {
			inside++
		}
		if r.Case.Want.Op == browser.OpClick {
			clicks++
			if r.Inside {
				clicksInside++
			}
		}
	}
	b.WriteString("\n## Arm c, a box question checked against the element rect\n\n")
	fmt.Fprintf(&b, "**%d of %d click targets have the box centre inside the element rect.** Over every targeted element, clicks, typing and selects together: %d of %d.\n\n", clicksInside, clicks, inside, len(boxes))
	b.WriteString("Edges are fractions of the screenshot, left right top bottom. The rect is the element's getBoundingClientRect over the viewport.\n\n")
	b.WriteString("| page | op | element | rect | box | centre inside | confidence | input | wall | dollars | error |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range boxes {
		fmt.Fprintf(&b, "| %s | %s | %d %q | %s | %s | %t | %.2f | %d | %.0f ms | $%.6f | %s |\n",
			r.Case.ID, r.Case.Want.Op, r.Case.Want.Element, r.Label, edges(r.Rect), edges(r.Box), r.Inside, r.Confidence, r.Input, millis(r.Latency), float64(r.Input)*jevDollarsPerMillionInput/1e6, r.Error)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func got(r Reading) string {
	if r.Error != "" {
		return "none"
	}
	return action(r.Got)
}

func edges(r Rect) string {
	return fmt.Sprintf("%.3f %.3f %.3f %.3f", r.Left, r.Right, r.Top, r.Bottom)
}
