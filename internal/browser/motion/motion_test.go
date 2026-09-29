package motion

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"tofu/internal/konst"
)

func ms(v float64) *float64 { return &v }

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	return img
}

func tileColour(i int) color.RGBA {
	return color.RGBA{R: uint8(70 + 6*i), G: uint8(190 - 5*i), B: uint8(90 + 4*(i%5)), A: 255}
}

func scenario(width, height int) Scenario {
	return Scenario{
		URL:      "http://localhost:4173/",
		Viewport: Viewport{Width: width, Height: height},
		Trigger:  Action{Action: "click", Ref: "e12"},
		Watch:    []Watch{{Name: "panel", Ref: "e4"}, {Name: "badge", Ref: "e5"}},
	}
}

func saveTake(t *testing.T, root string, sc Scenario, images []image.Image, frameMs func(int) float64, trace []Sample) Take {
	t.Helper()
	frames := make([]Frame, len(images))
	jpegs := make([][]byte, len(images))
	for i, img := range images {
		frames[i] = Frame{ChromeTimestampS: 1000 + frameMs(i)/1000, MsFromTrigger: ms(frameMs(i))}
		jpegs[i] = encodeJPEG(t, img)
	}
	id := NewID(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC), "Menu Close", 1)
	take, err := Save(root, Take{
		Manifest: Manifest{TakeID: id, Scenario: sc, Trigger: &Trigger{Event: "click", WallMs: 1_000_000}, Browser: "Chrome/140"},
		Frames:   frames,
		Trace:    trace,
	}, jpegs)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(root, take.Manifest.TakeID)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}

func jumpTrace() []Sample {
	var trace []Sample
	for k := 0; k <= 30; k++ {
		at := -29 + 17*float64(k)
		x := 100.0
		if k%2 == 1 {
			x = 100.3
		}
		if at >= 277 {
			x = 140
		}
		trace = append(trace, Sample{MsFromTrigger: ms(at), Elements: map[string]*Element{
			"panel": {X: x, Y: 50, Width: 200, Height: 80, Opacity: 1, Display: "block", Visibility: "visible"},
			"badge": {X: 10, Y: 10, Width: 20, Height: 20, Opacity: 1, Display: "block", Visibility: "visible"},
		}})
	}
	return append(trace, Sample{Elements: map[string]*Element{"panel": nil, "badge": nil}})
}

func syntheticTake(t *testing.T) Take {
	images := make([]image.Image, 20)
	for i := range images {
		images[i] = solid(320, 240, tileColour(i))
	}
	return saveTake(t, t.TempDir(), scenario(320, 240), images, func(i int) float64 { return -40 + 25*float64(i) }, jumpTrace())
}

func TestTableShowsOnlyTheJump(t *testing.T) {
	take := syntheticTake(t)
	w, err := WindowOf([]Take{take}, ms(0), ms(300))
	if err != nil {
		t.Fatal(err)
	}
	rows, samples := Table(take.Manifest.Scenario.Watch, take.Trace, w)
	want := []string{
		"ms      panel x",
		"+5.0    100.00",
		"+277.0  140.00",
		"+294.0  140.00",
	}
	if samples != 18 || strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("samples %d, table:\n%s\nwant:\n%s", samples, strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func isInk(c color.RGBA) bool { return c.R > 230 && c.G > 230 && c.B > 230 }

func inkMask(img *image.RGBA, origin image.Point, size image.Point) []bool {
	mask := make([]bool, 0, size.X*size.Y)
	for y := 0; y < size.Y; y++ {
		for x := 0; x < size.X; x++ {
			mask = append(mask, isInk(img.RGBAAt(origin.X+x, origin.Y+y)))
		}
	}
	return mask
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return d(a.R, b.R) < 12 && d(a.G, b.G) < 12 && d(a.B, b.B) < 12
}

func decodeSheet(t *testing.T, s Sheet) *image.RGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(s.PNG))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() > konst.MotionSheetMaxPx || b.Dy() > konst.MotionSheetMaxPx || len(s.PNG) > konst.MotionSheetMaxBytes {
		t.Fatalf("sheet %dx%d, %d bytes", b.Dx(), b.Dy(), len(s.PNG))
	}
	rgba := image.NewRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}

func TestInspectSheetHasOneLabelledTilePerFrame(t *testing.T) {
	take := syntheticTake(t)
	w, err := WindowOf([]Take{take}, ms(0), ms(300))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inspect(take, w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sheets) != 1 || got.Sheets[0].Frames != 13 {
		t.Fatalf("got %d sheets, first holds %d frames", len(got.Sheets), got.Sheets[0].Frames)
	}
	sheet := decodeSheet(t, got.Sheets[0])
	labelSize := image.Pt(200, 40)
	step := konst.MotionInspectTilePx + konst.MotionSheetGapPx
	for slot := range 16 {
		origin := image.Pt(slot%4*step, slot/4*(300+konst.MotionSheetGapPx))
		mask := inkMask(sheet, origin, labelSize)
		if slot >= 13 {
			if near(sheet.RGBAAt(origin.X+200, origin.Y+150), color.RGBA{255, 255, 255, 255}) {
				continue
			}
			t.Fatalf("slot %d is not empty", slot)
		}
		frame := slot + 2
		if centre := sheet.RGBAAt(origin.X+200, origin.Y+150); !near(centre, tileColour(frame)) {
			t.Fatalf("slot %d centre %v, want frame %d colour %v", slot, centre, frame+1, tileColour(frame))
		}
		expected := solid(labelSize.X, labelSize.Y, tileColour(frame))
		text := "+" + itoa(-40+25*frame) + "ms #" + itoa(frame+1)
		drawLabel(expected, text, konst.MotionInspectLabelScale)
		want := inkMask(expected, image.Point{}, labelSize)
		for i := range want {
			if want[i] != mask[i] {
				t.Fatalf("slot %d label differs from %q at pixel %d", slot, text, i)
			}
		}
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func TestHeavySheetIsSplitInTwo(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	images := make([]image.Image, 16)
	for i := range images {
		img := solid(400, 300, color.RGBA{})
		for p := 0; p < len(img.Pix); p += 4 {
			img.Pix[p], img.Pix[p+1], img.Pix[p+2] = uint8(rng.IntN(256)), uint8(rng.IntN(256)), uint8(rng.IntN(256))
		}
		images[i] = img
	}
	take := saveTake(t, t.TempDir(), scenario(400, 300), images, func(i int) float64 { return float64(16 * i) }, jumpTrace())
	w, err := WindowOf([]Take{take}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inspect(take, w, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sheets) != 2 || got.Sheets[0].Frames+got.Sheets[1].Frames != 16 {
		t.Fatalf("got %d sheets", len(got.Sheets))
	}
	for _, s := range got.Sheets {
		decodeSheet(t, s)
	}
}

func TestCompareRefusesDifferentViewports(t *testing.T) {
	frame := []image.Image{solid(320, 240, tileColour(0))}
	at := func(int) float64 { return 20 }
	before := saveTake(t, t.TempDir(), scenario(320, 240), frame, at, jumpTrace())
	same := saveTake(t, t.TempDir(), scenario(320, 240), frame, at, jumpTrace())
	wider := saveTake(t, t.TempDir(), scenario(400, 240), frame, at, jumpTrace())
	if _, err := Compare([]Take{before}, []Take{same}, nil, nil, nil); err != nil {
		t.Fatalf("matching pair refused: %v", err)
	}
	_, err := Compare([]Take{before}, []Take{wider}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "viewport differs") {
		t.Fatalf("got %v", err)
	}
}

func TestLabelDrawsEveryGlyph(t *testing.T) {
	const text = "+277ms #12"
	const scale = 2
	img := solid(200, 30, color.RGBA{120, 120, 120, 255})
	drawLabel(img, text, scale)
	for i, r := range text {
		if r == ' ' {
			continue
		}
		ink := 0
		for y := 0; y < 30; y++ {
			for x := range glyphWidth * scale {
				if isInk(img.RGBAAt(labelTextX(i, scale)+x, y)) {
					ink++
				}
			}
		}
		if ink == 0 {
			t.Fatalf("glyph %q at %d drew nothing", r, i)
		}
	}
}
