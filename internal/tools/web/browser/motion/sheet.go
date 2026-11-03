package motion

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"tofu/internal/konst"
)

type Crop struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Sheet struct {
	PNG    []byte
	FromMs float64
	ToMs   float64
	Frames int
	Rows   []string
}

type tile struct {
	path  string
	label string
	ms    float64
}

type layout struct {
	cols  int
	box   image.Rectangle
	w, h  int
	scale int
}

func (w Window) tiles(t Take) []tile {
	var in []tile
	for i, f := range t.Frames {
		if w.holds(f.MsFromTrigger, konst.MotionFrameLagMillis) {
			label := fmt.Sprintf("%+dms #%d", int(math.Round(*f.MsFromTrigger)), i+1)
			in = append(in, tile{path: filepath.Join(t.Dir, f.File), label: label, ms: *f.MsFromTrigger})
		}
	}
	return in
}

func even(n float64) int {
	return max(2, int(math.Round(n/2))*2)
}

func layoutFor(framePath string, vp Viewport, crop *Crop, tilePx, cols, scale int) (layout, string, error) {
	data, err := os.ReadFile(framePath)
	if err != nil {
		return layout{}, "", err
	}
	size, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return layout{}, "", fmt.Errorf("%s: %w", framePath, err)
	}
	box, warning := image.Rect(0, 0, size.Width, size.Height), ""
	if crop != nil {
		k := float64(size.Width) / float64(vp.Width)
		asked := image.Rect(0, 0, int(math.Round(crop.W*k)), int(math.Round(crop.H*k))).Add(image.Pt(int(math.Round(crop.X*k)), int(math.Round(crop.Y*k))))
		box = asked.Intersect(box)
		if box.Dx() < 2 || box.Dy() < 2 {
			return layout{}, "", fmt.Errorf("crop lies outside the %dx%d frame", size.Width, size.Height)
		}
		if box != asked {
			warning = fmt.Sprintf("crop clamped to the %dx%d frame", size.Width, size.Height)
		}
	}
	lay := layout{cols: cols, box: box, w: tilePx, h: even(float64(tilePx*box.Dy()) / float64(box.Dx())), scale: scale}
	if lay.h > konst.MotionSheetMaxPx {
		lay.w, lay.h = even(float64(konst.MotionSheetMaxPx*box.Dx())/float64(box.Dy())), konst.MotionSheetMaxPx
	}
	return lay, warning, nil
}

func (lay layout) render(t tile) (*image.RGBA, error) {
	out := image.NewRGBA(image.Rect(0, 0, lay.w, lay.h))
	if t.path == "" {
		draw.Draw(out, out.Rect, image.NewUniform(color.Gray{Y: konst.MotionEmptyTileGrey}), image.Point{}, draw.Src)
		return out, nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return nil, err
	}
	frame, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.path, err)
	}
	src := image.NewRGBA(image.Rect(0, 0, lay.box.Dx(), lay.box.Dy()))
	draw.Draw(src, src.Rect, frame, lay.box.Min, draw.Src)
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	for y := range lay.h {
		y0, y1 := y*sh/lay.h, max((y+1)*sh/lay.h, y*sh/lay.h+1)
		for x := range lay.w {
			x0, x1 := x*sw/lay.w, max((x+1)*sw/lay.w, x*sw/lay.w+1)
			var sum [3]int
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					p := src.PixOffset(sx, sy)
					sum[0], sum[1], sum[2] = sum[0]+int(src.Pix[p]), sum[1]+int(src.Pix[p+1]), sum[2]+int(src.Pix[p+2])
				}
			}
			n, d := (y1-y0)*(x1-x0), out.PixOffset(x, y)
			out.Pix[d], out.Pix[d+1], out.Pix[d+2], out.Pix[d+3] = uint8(sum[0]/n), uint8(sum[1]/n), uint8(sum[2]/n), 255
		}
	}
	drawLabel(out, t.label, lay.scale)
	return out, nil
}

func (lay layout) sheet(rows [][]tile) ([]byte, error) {
	gap := konst.MotionSheetGapPx
	img := image.NewRGBA(image.Rect(0, 0, lay.cols*(lay.w+gap)-gap, len(rows)*(lay.h+gap)-gap))
	draw.Draw(img, img.Rect, image.White, image.Point{}, draw.Src)
	for r, row := range rows {
		for c, t := range row {
			rendered, err := lay.render(t)
			if err != nil {
				return nil, err
			}
			at := image.Pt(c*(lay.w+gap), r*(lay.h+gap))
			draw.Draw(img, rendered.Rect.Add(at), rendered, image.Point{}, draw.Src)
		}
	}
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}

type renderedSheet struct {
	png  []byte
	rows []int
}

func (lay layout) sheets(rows [][]tile) ([]renderedSheet, error) {
	var out []renderedSheet
	var render func(idx []int) error
	render = func(idx []int) error {
		picked := make([][]tile, len(idx))
		for i, k := range idx {
			picked[i] = rows[k]
		}
		data, err := lay.sheet(picked)
		if err != nil {
			return err
		}
		if len(data) > konst.MotionSheetMaxBytes && len(idx) > 1 {
			half := (len(idx) + 1) / 2
			return errors.Join(render(idx[:half]), render(idx[half:]))
		}
		out = append(out, renderedSheet{png: data, rows: idx})
		return nil
	}
	perSheet := max(1, (konst.MotionSheetMaxPx+konst.MotionSheetGapPx)/(lay.h+konst.MotionSheetGapPx))
	for start := 0; start < len(rows); start += perSheet {
		idx := make([]int, 0, perSheet)
		for k := start; k < min(start+perSheet, len(rows)); k++ {
			idx = append(idx, k)
		}
		if err := render(idx); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type Inspection struct {
	Table    []string
	Samples  int
	Sheets   []Sheet
	Warnings []string
}

func Inspect(t Take, w Window, crop *Crop) (Inspection, error) {
	var got Inspection
	got.Table, got.Samples = t.table(blinking([]Take{t}), w)
	if len(t.Manifest.Scenario.Watch) > 0 && got.Samples == 0 {
		return got, fmt.Errorf("no trace samples between %g and %g ms; widen from_ms and to_ms", w.FromMs, w.ToMs)
	}
	tiles := w.tiles(t)
	if len(tiles) == 0 {
		return got, nil
	}
	lay, warning, err := layoutFor(tiles[0].path, t.Manifest.Scenario.Viewport, crop, konst.MotionInspectTilePx, konst.MotionInspectColumns, konst.MotionInspectLabelScale)
	if err != nil {
		return got, err
	}
	if warning != "" {
		got.Warnings = append(got.Warnings, warning)
	}
	var rows [][]tile
	for i := 0; i < len(tiles); i += lay.cols {
		rows = append(rows, tiles[i:min(i+lay.cols, len(tiles))])
	}
	rendered, err := lay.sheets(rows)
	if err != nil {
		return got, err
	}
	for _, r := range rendered {
		first, last := rows[r.rows[0]], rows[r.rows[len(r.rows)-1]]
		s := Sheet{PNG: r.png, FromMs: first[0].ms, ToMs: last[len(last)-1].ms}
		for _, k := range r.rows {
			s.Frames += len(rows[k])
		}
		got.Sheets = append(got.Sheets, s)
	}
	return got, nil
}
