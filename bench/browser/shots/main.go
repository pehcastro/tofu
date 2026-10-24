package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/jpeg"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	browserbench "tofu/bench/browser"
	"tofu/internal/browser"
)

const (
	profile      = "shots"
	viewportW    = 1280
	viewportH    = 800
	paintWait    = 600 * time.Millisecond
	readTimeout  = 5 * time.Second
	rectsProgram = `(() => ({userAgent: navigator.userAgent, width: innerWidth, height: innerHeight,
  rects: Object.fromEntries([...(window.__tofu?.nodes || new Map())].map(([i, e]) => {
    const r = e.getBoundingClientRect();
    return [i, {left: r.left, top: r.top, right: r.right, bottom: r.bottom}];
  }))}))()`
)

type fill struct {
	element int
	value   string
}

func main() {
	pages := flag.String("pages", filepath.Join("bench", "browser", "pages"), "the recorded pages, where the shots are written beside them")
	fastbrowse := flag.String("fastbrowse", "", "fastbrowse's src/fastbrowse/evals/fixtures directory")
	hotel := flag.String("hotel", "", "jev-ultrafast's jev_ultrafast/static directory")
	home := flag.String("home", "", "a fresh home for the headless profile, removed afterwards")
	flag.Parse()
	if *fastbrowse == "" || *hotel == "" || *home == "" {
		fmt.Fprintln(os.Stderr, "shots: -fastbrowse, -hotel and -home are required")
		os.Exit(2)
	}
	if err := record(*pages, *fastbrowse, *hotel, *home); err != nil {
		fmt.Fprintln(os.Stderr, "shots:", err)
		os.Exit(1)
	}
}

func record(pagesDir, fastbrowse, hotel, home string) (err error) {
	cases, err := browserbench.Load(pagesDir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/fixture.html", http.FileServer(http.Dir(hotel)))
	mux.Handle("/", http.FileServer(http.Dir(fastbrowse)))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: readTimeout}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	origin := "http://" + listener.Addr().String()

	if _, err := browser.NewProfile(home, profile); err != nil {
		return err
	}
	client, err := browser.Endpoint{Home: home, Profile: profile, Headless: true}.Dial()
	if err != nil {
		return err
	}
	defer func() {
		_ = client.Close()
		_, stopped := browser.StopProfile(home, profile)
		err = errors.Join(err, stopped)
	}()
	tab, err := client.Open(origin + "/shop.html")
	if err != nil {
		return err
	}
	shared := browser.SharedTab{Client: client, ID: tab}
	machine, _ := os.Hostname()
	out := browserbench.Shots{
		Date: time.Now().Format(time.DateOnly), Machine: machine,
		Recorder: "go run ./bench/browser/shots, tofu's own headless Chrome through browser.Endpoint and the direct CDP path, a fresh home per run",
		Viewport: fmt.Sprintf("%dx%d at device scale 1", viewportW, viewportH),
		Frame:    "the last Page.screencastFrame after the snapshot, JPEG, rects in CSS pixels from getBoundingClientRect over the snapshot's own element ids",
	}
	fills := map[string][]fill{"contact": {{1, "Ada Lovelace"}, {2, "ada@example.com"}, {4, "My kettle arrived broken."}}}
	for _, c := range cases {
		recorded, _ := url.Parse(c.Page.URL)
		root := fastbrowse
		if recorded.Path == "/fixture.html" {
			root = hotel
		}
		source := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(recorded.Path, "/")))
		html, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := shared.Navigate(origin + recorded.RequestURI()); err != nil {
			return err
		}
		if _, err := cdp(client, tab, "Emulation.setDeviceMetricsOverride", map[string]any{"width": viewportW, "height": viewportH, "deviceScaleFactor": 1, "mobile": false}); err != nil {
			return err
		}
		live, err := shared.Snapshot(context.Background())
		if err != nil {
			return err
		}
		for _, typed := range fills[c.ID] {
			if _, err := shared.Drive(live, browser.Action{Op: browser.OpTypeText, Element: typed.element, Value: typed.value}); err != nil {
				return fmt.Errorf("%s: %w", c.ID, err)
			}
		}
		if live, err = shared.Snapshot(context.Background()); err != nil {
			return err
		}
		raw, err := cdp(client, tab, "Runtime.evaluate", map[string]any{"expression": rectsProgram, "returnByValue": true})
		if err != nil {
			return err
		}
		var measured struct {
			Result struct {
				Value struct {
					UserAgent string                       `json:"userAgent"`
					Width     int                          `json:"width"`
					Height    int                          `json:"height"`
					Rects     map[string]browserbench.Rect `json:"rects"`
				} `json:"value"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &measured); err != nil {
			return err
		}
		frame, err := screenshot(client, tab)
		if err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		size, err := jpeg.DecodeConfig(bytes.NewReader(frame))
		if err != nil {
			return err
		}
		name := c.ID + ".jpg"
		if err := os.WriteFile(filepath.Join(pagesDir, name), frame, 0o644); err != nil {
			return err
		}
		value := measured.Result.Value
		out.Browser = value.UserAgent
		differences := differ(c.Page, live)
		out.Pages = append(out.Pages, browserbench.Shot{
			ID: c.ID, Source: c.Source, SourceSHA: digest(html), Image: name, ImageSHA: digest(frame),
			Width: size.Width, Height: size.Height, Viewport: [2]int{value.Width, value.Height},
			Rects: value.Rects, Matches: len(differences) == 0, Differences: differences,
		})
		fmt.Printf("%s: %dx%d, %d rects, matches the recorded page: %t %s\n", c.ID, size.Width, size.Height, len(value.Rects), len(differences) == 0, strings.Join(differences, "; "))
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pagesDir, "shots.json"), append(encoded, '\n'), 0o644)
}

func cdp(client *browser.Client, tab int, method string, params any) (json.RawMessage, error) {
	args, err := json.Marshal(map[string]any{"calls": []map[string]any{{"method": method, "params": params}}})
	if err != nil {
		return nil, err
	}
	raw, err := client.Call(tab, "cdp", args)
	if err != nil {
		return nil, err
	}
	var answers []struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &answers); err != nil || len(answers) != 1 {
		return nil, fmt.Errorf("%s answered %.200s", method, raw)
	}
	if answers[0].Error != "" {
		return nil, fmt.Errorf("%s: %s", method, answers[0].Error)
	}
	return answers[0].Result, nil
}

func screenshot(client *browser.Client, tab int) ([]byte, error) {
	if err := client.StartScreencast(tab); err != nil {
		return nil, err
	}
	time.Sleep(paintWait)
	frames, err := client.StopScreencast(tab)
	if err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, errors.New("the screencast painted no frame")
	}
	return frames[len(frames)-1].Data, nil
}

func differ(recorded, live browser.Page) []string {
	var differences []string
	if recorded.Title != live.Title {
		differences = append(differences, fmt.Sprintf("title %q against %q", live.Title, recorded.Title))
	}
	if recorded.Text != live.Text {
		was, now := []rune(recorded.Text), []rune(live.Text)
		at := 0
		for at < min(len(was), len(now)) && was[at] == now[at] {
			at++
		}
		differences = append(differences, fmt.Sprintf("text from rune %d reads %q against %q", at, string(now[at:min(at+24, len(now))]), string(was[at:min(at+24, len(was))])))
	}
	if len(recorded.Elements) != len(live.Elements) {
		return append(differences, fmt.Sprintf("%d elements against %d", len(live.Elements), len(recorded.Elements)))
	}
	for i, want := range recorded.Elements {
		got := live.Elements[i]
		if got.Index != want.Index || got.Role != want.Role || got.Label != want.Label || got.Value != want.Value {
			differences = append(differences, fmt.Sprintf("element %d is %s %q %q against %s %q %q", want.Index, got.Role, got.Label, got.Value, want.Role, want.Label, want.Value))
		}
	}
	return differences
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
