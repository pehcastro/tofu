package motion

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
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
		Trigger:  Action{Action: "click", Selector: ".menu button"},
		Watch:    []Watch{{Name: "panel", Selector: ".menu .panel"}, {Name: "badge", Selector: ".menu .badge"}},
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

func TestTheFieldNotesScenarioParsesWithReadyAttributesAndScale(t *testing.T) {
	raw, err := os.ReadFile("testdata/field-notes-close.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var parsed Scenario
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	sc, err := parsed.Checked()
	if err != nil {
		t.Fatal(err)
	}
	if sc.Ready != (Ready{Selector: ".faq .item:nth-child(1) .content[data-state='open']", SettleMs: 350}) ||
		!slices.Equal(sc.Watch[0].Attributes, []string{"data-state"}) || sc.Watch[1].Selector != ".faq .item:nth-child(2) .header" ||
		sc.Viewport != (Viewport{Width: 960, Height: 720, DeviceScaleFactor: 1}) || sc.ReducedMotion != "no-preference" ||
		sc.Trigger != (Action{Action: "click", Role: "button", Name: "What comes with a Field Notes membership?", Exact: true}) ||
		sc.Name != "field-notes-close" || sc.RecordBeforeMs != 300 || sc.RecordAfterMs != 800 {
		t.Fatalf("Field Notes parsed as %+v", sc)
	}
	for given, want := range map[Viewport]Viewport{{}: {960, 720, 1}, {DeviceScaleFactor: 2}: {960, 720, 2}, {Width: 400, Height: 300}: {400, 300, 1}} {
		base := scenario(0, 0)
		base.Viewport = given
		if got, err := base.Checked(); err != nil || got.Viewport != want {
			t.Errorf("viewport %+v checked as %+v, %v; want %+v", given, got.Viewport, err, want)
		}
	}
	for name, broken := range map[string]func(*Scenario){
		"a negative scale":         func(s *Scenario) { s.Viewport.DeviceScaleFactor = -1 },
		"a negative settle":        func(s *Scenario) { s.Ready.SettleMs = -1 },
		"an unnamed attribute":     func(s *Scenario) { s.Watch[0].Attributes = []string{""} },
		"an attribute twice":       func(s *Scenario) { s.Watch[0].Attributes = []string{"data-state", "data-state"} },
		"a watch with no selector": func(s *Scenario) { s.Watch[0].Selector = "" },
		"a role and a selector": func(s *Scenario) {
			s.Trigger = Action{Action: "click", Role: "button", Name: "Close", Selector: ".close"}
		},
		"a role with no name":      func(s *Scenario) { s.Trigger = Action{Action: "click", Role: "button"} },
		"a name with no role":      func(s *Scenario) { s.Trigger = Action{Action: "click", Name: "Close"} },
		"a click on nothing":       func(s *Scenario) { s.Trigger = Action{Action: "click"} },
		"exact with no name":       func(s *Scenario) { s.Trigger = Action{Action: "click", Selector: ".close", Exact: true} },
		"a setup hover on nothing": func(s *Scenario) { s.Setup = []Action{{Action: "hover"}} },
		"an unknown motion":        func(s *Scenario) { s.ReducedMotion = "sometimes" },
	} {
		s := scenario(320, 240)
		s.Watch = []Watch{{Name: "panel", Selector: ".menu .panel"}}
		broken(&s)
		if _, err := s.Checked(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestAWatchedAttributeIsATableColumnAndSurvivesTheStore(t *testing.T) {
	sc := scenario(320, 240)
	sc.Watch = []Watch{{Name: "first-answer", Selector: ".faq .item:nth-child(1) .content", Attributes: []string{"data-state"}}}
	at := func(ms float64, state ...string) Sample {
		e := &Element{Width: 200, Height: 80, Opacity: 1, Display: "block", Visibility: "visible", Attributes: map[string]string{}}
		for _, s := range state {
			e.Attributes["data-state"] = s
		}
		return Sample{MsFromTrigger: &ms, Elements: map[string]*Element{"first-answer": e}}
	}
	id := NewID(time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC), "field notes", 1)
	root := t.TempDir()
	if _, err := Save(root, Take{
		Manifest: Manifest{TakeID: id, Scenario: sc, Trigger: &Trigger{Event: "pointerdown", WallMs: 1_000_000, TimeStamp: 1234.5}},
		Trace:    []Sample{at(0, "open"), at(16, "open"), at(33, "closed"), at(50)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	take, err := Load(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if take.Manifest.Trigger.TimeStamp != 1234.5 || !slices.Equal(take.Manifest.Scenario.Watch[0].Attributes, []string{"data-state"}) {
		t.Fatalf("the store returned trigger %+v and watch %+v", take.Manifest.Trigger, take.Manifest.Scenario.Watch)
	}
	w, err := WindowOf([]Take{take}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := Table(take.Manifest.Scenario.Watch, take.Trace, w)
	want := []string{
		"ms     first-answer data-state",
		"+0.0   open",
		"+33.0  closed",
		"+50.0  null",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Fatalf("table:\n%s\nwant:\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func TestAReopenThatPushesTheRowsBelowIsLedByTheAnswer(t *testing.T) {
	box := func(y, h float64) *Element {
		return &Element{X: 140, Y: y, Width: 680, Height: h, Opacity: 1, Display: "block", Visibility: "visible"}
	}
	elements := []Discovered{
		{Selector: "#root > main:nth-child(1)", Role: "main", Parent: -1},
		{Selector: ".faq", Role: "div", Parent: 0},
		{Selector: ".item1", Role: "div", Parent: 1},
		{Selector: ".header1", Role: "h3", Parent: 2},
		{Selector: ".content1", Role: "div", Name: "A quiet place", Parent: 2},
		{Selector: ".answer1", Role: "div", Name: "A quiet place", Parent: 4},
		{Selector: ".item2", Role: "div", Parent: 1},
		{Selector: ".footer", Role: "p", Parent: 0},
	}
	var trace []Sample
	for k, answer := range []float64{5, 2.5, 1.2, 0.59, 76.78, -1, -1, -1} {
		open, content, inner := max(answer, 0), box(300, answer), box(300, 100)
		if answer < 0 {
			content = &Element{Opacity: 1, Display: "none", Visibility: "visible"}
			inner = &Element{Opacity: 1, Display: "block", Visibility: "visible"}
		}
		trace = append(trace, Sample{MsFromTrigger: ms([]float64{200, 216, 232, 240, 258, 274, 290, 306}[k]), Elements: map[string]*Element{
			"0": box(64, 600+open), "1": box(233, 134+open), "2": box(233, 67+open), "3": box(233, 67),
			"4": content, "5": inner, "6": box(300+open, 67), "7": box(500+open, 20),
		}})
	}
	var frames []Frame
	for _, at := range []float64{230, 246, 262, 278, 294, 310} {
		frames = append(frames, Frame{MsFromTrigger: ms(at)})
	}
	report := Report(Take{Manifest: Manifest{Discovery: &Discovery{Seen: 8, Cap: 250, Elements: elements}}, Frames: frames, Trace: trace})
	want := `1. div "A quiet place" at .content1: +258.0 ms for 16.0 ms (1 paint), frames #3 #4 #5; `
	if len(report) != 2 || report[0] != "watched 8 of 8 elements seen" || !strings.HasPrefix(report[1], want) ||
		!strings.Contains(report[1], "display block -> block -> none") || !strings.HasSuffix(report[1], "; 3 more elements changed with it") {
		t.Fatalf("report %q; want the answer first as %q, its display, and the item and two rows that moved folded into it", report, want)
	}
}

func TestInspectAndCompareOfDiscoveredTakesTableWhatBlinkedBySelectorNotByIndex(t *testing.T) {
	root := t.TempDir()
	sc := scenario(96, 72)
	sc.Watch = nil
	discovered := func(flashing string, order ...string) Take {
		var elements []Discovered
		for _, selector := range order {
			elements = append(elements, Discovered{Selector: selector, Role: "button", Name: selector, Parent: -1})
		}
		var trace []Sample
		for k := range 8 {
			sample := Sample{MsFromTrigger: ms(float64(k) * 16.7), Elements: map[string]*Element{}}
			for i, selector := range order {
				visibility := "visible"
				if selector == flashing && k == 4 {
					visibility = "hidden"
				}
				sample.Elements[strconv.Itoa(i)] = &Element{X: float64(40 * i), Width: 30, Height: 20, Opacity: 1, Display: "block", Visibility: visibility}
			}
			trace = append(trace, sample)
		}
		take := saveTake(t, root, sc, []image.Image{solid(96, 72, tileColour(0))}, func(int) float64 { return 60 }, trace)
		take.Manifest.Discovery = &Discovery{Seen: 2, Cap: 250, Elements: elements}
		return take
	}
	inspected := discovered("#save", "#other", "#save")
	w, err := WindowOf([]Take{inspected}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := Inspect(inspected, w, nil)
	if table := strings.Join(seen.Table, "\n"); err != nil || !strings.Contains(table, "#save visibility") || !strings.Contains(table, "hidden") || strings.Contains(table, "#other") {
		t.Fatalf("inspect table:\n%s\n%v; want the #save column going hidden and nothing for #other", table, err)
	}
	got, err := Compare([]Take{discovered("#save", "#other", "#save")}, []Take{discovered("#other", "#save", "#other")}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{strings.Join(got.Takes[0].Table, "\n"), strings.Join(got.Takes[1].Table, "\n")}
	if !strings.Contains(tables[0], "#save visibility") || !strings.Contains(tables[0], "hidden") || !strings.Contains(tables[1], "#other visibility") ||
		!strings.Contains(tables[1], "hidden") || strings.Contains(tables[0], "#other visibility") || strings.Contains(tables[1], "#save visibility") {
		t.Fatalf("before table:\n%s\nafter table:\n%s\nwant #save hidden only before and #other hidden only after", tables[0], tables[1])
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
